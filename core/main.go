// core: workspace lifecycle service. Owns creating, uploading into,
// downloading from, listing, and deleting workspaces on shared disk at
// WORKSPACE_ROOT. Every other service in this repo (split_pdf, detect_boxes,
// crop_boxes, read_qr_ocr, gemini_ocr) reads/writes workspace files directly
// against that same shared WORKSPACE_ROOT rather than calling this service's
// HTTP API for ordinary file I/O — core is only the lifecycle owner.
//
// Unlike the other services' single POST /tasks endpoint, core exposes a
// small CRUD REST API (create/upload/download/list/delete workspaces) since
// its job is stateful lifecycle management, not a one-shot compute task.
// Built on huma (https://huma.rocks) over the stdlib net/http mux, so the
// API is self-documenting: GET /docs (Swagger UI) and GET /openapi.json are
// generated from the input/output structs below, not hand-maintained.
package main

import (
	"archive/zip"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

const defaultTTLHours = 24 * 7
const defaultSweepIntervalMinutes = 60

type server struct {
	root string
	ttl  time.Duration
}

func main() {
	host := flag.String("host", "0.0.0.0", "address to listen on")
	port := flag.Int("port", 8824, "port to listen on")
	flag.Parse()

	workspaceRoot := os.Getenv("WORKSPACE_ROOT")
	if workspaceRoot == "" {
		log.Fatal("WORKSPACE_ROOT must be set")
	}
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		log.Fatalf("create WORKSPACE_ROOT %s: %v", workspaceRoot, err)
	}

	ttl := time.Duration(envInt("TTL_HOURS", defaultTTLHours)) * time.Hour
	sweepInterval := time.Duration(envInt("SWEEP_INTERVAL_MINUTES", defaultSweepIntervalMinutes)) * time.Minute

	srv := &server{root: workspaceRoot, ttl: ttl}

	go startSweep(workspaceRoot, sweepInterval, ttl)

	mux := http.NewServeMux()
	config := huma.DefaultConfig("Taksu Vision Core", "1.0.0")
	config.Info.Description = "Workspace lifecycle: create, upload into, download from, list, and delete workspaces on shared disk."
	// Stoplight Elements (huma's default /docs renderer) doesn't support a
	// "Try it" file picker for array-of-files multipart fields like our
	// upload-files operation's `files` field; Swagger UI does.
	config.DocsRenderer = huma.DocsRendererSwaggerUI
	api := humago.New(mux, config)
	srv.registerRoutes(api)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Printf("core listening on %s (WORKSPACE_ROOT=%s, ttl=%s, docs at /docs)", addr, workspaceRoot, ttl)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func envInt(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("%s must be an integer, got %q", name, v)
	}
	return n
}

func (s *server) registerRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Tags:        []string{"health"},
	}, s.healthHandler)

	huma.Register(api, huma.Operation{
		OperationID: "create-workspace",
		Method:      http.MethodPost,
		Path:        "/workspaces",
		Summary:     "Create a workspace",
		Tags:        []string{"workspaces"},
	}, s.createWorkspaceHandler)

	huma.Register(api, huma.Operation{
		OperationID: "get-workspace",
		Method:      http.MethodGet,
		Path:        "/workspaces/{id}",
		Summary:     "Get workspace metadata and its file listing",
		Tags:        []string{"workspaces"},
	}, s.getWorkspaceHandler)

	huma.Register(api, huma.Operation{
		OperationID: "delete-workspace",
		Method:      http.MethodDelete,
		Path:        "/workspaces/{id}",
		Summary:     "Delete a workspace and everything in it",
		Tags:        []string{"workspaces"},
	}, s.deleteWorkspaceHandler)

	huma.Register(api, huma.Operation{
		OperationID: "upload-files",
		Method:      http.MethodPost,
		Path:        "/workspaces/{id}/files",
		Summary:     "Upload one or more files into a workspace",
		Tags:        []string{"files"},
	}, s.uploadFilesHandler)

	huma.Register(api, huma.Operation{
		OperationID: "download-file",
		Method:      http.MethodGet,
		Path:        "/workspaces/{id}/files",
		Summary:     "Download a single file from a workspace",
		Tags:        []string{"files"},
	}, s.downloadFileHandler)

	huma.Register(api, huma.Operation{
		OperationID: "download-archive",
		Method:      http.MethodGet,
		Path:        "/workspaces/{id}/archive",
		Summary:     "Download a workspace (or a subdirectory of it) as a zip archive",
		Tags:        []string{"files"},
	}, s.downloadArchiveHandler)
}

// --- health ---

type HealthOutput struct {
	Body struct {
		Status         string `json:"status" example:"ok"`
		WorkspaceRoot  string `json:"workspace_root"`
		WorkspaceCount int    `json:"workspace_count"`
	}
}

func (s *server) healthHandler(ctx context.Context, input *struct{}) (*HealthOutput, error) {
	entries, err := os.ReadDir(s.root)
	count := 0
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				count++
			}
		}
	}
	resp := &HealthOutput{}
	resp.Body.Status = "ok"
	resp.Body.WorkspaceRoot = s.root
	resp.Body.WorkspaceCount = count
	return resp, nil
}

// --- create workspace ---

type CreateWorkspaceOutput struct {
	Status int
	Body   struct {
		WorkspaceID string `json:"workspace_id"`
		CreatedAt   string `json:"created_at"`
		ExpiresAt   string `json:"expires_at"`
	}
}

func (s *server) createWorkspaceHandler(ctx context.Context, input *struct{}) (*CreateWorkspaceOutput, error) {
	meta, err := createWorkspace(s.root, s.ttl)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &CreateWorkspaceOutput{Status: http.StatusCreated}
	resp.Body.WorkspaceID = meta.WorkspaceID
	resp.Body.CreatedAt = meta.CreatedAt.Format(time.RFC3339Nano)
	resp.Body.ExpiresAt = meta.ExpiresAt.Format(time.RFC3339Nano)
	return resp, nil
}

// --- get workspace ---

type WorkspaceIDInput struct {
	ID string `path:"id" doc:"Workspace ID"`
}

type GetWorkspaceOutput struct {
	Body struct {
		WorkspaceID string      `json:"workspace_id"`
		CreatedAt   string      `json:"created_at"`
		ExpiresAt   string      `json:"expires_at"`
		Files       []FileEntry `json:"files"`
	}
}

func (s *server) getWorkspaceHandler(ctx context.Context, input *WorkspaceIDInput) (*GetWorkspaceOutput, error) {
	meta, err := loadWorkspaceMeta(s.root, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}
	files, err := listWorkspaceFiles(s.root, input.ID, "")
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &GetWorkspaceOutput{}
	resp.Body.WorkspaceID = meta.WorkspaceID
	resp.Body.CreatedAt = meta.CreatedAt.Format(time.RFC3339Nano)
	resp.Body.ExpiresAt = meta.ExpiresAt.Format(time.RFC3339Nano)
	resp.Body.Files = files
	return resp, nil
}

// --- delete workspace ---

type DeleteWorkspaceOutput struct {
	Body struct {
		WorkspaceID string `json:"workspace_id"`
		Deleted     bool   `json:"deleted"`
	}
}

func (s *server) deleteWorkspaceHandler(ctx context.Context, input *WorkspaceIDInput) (*DeleteWorkspaceOutput, error) {
	if err := deleteWorkspace(s.root, input.ID); err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}
	resp := &DeleteWorkspaceOutput{}
	resp.Body.WorkspaceID = input.ID
	resp.Body.Deleted = true
	return resp, nil
}

// --- upload files ---

// UploadForm's `files` field is typed (not raw multipart.Form) so huma can
// describe it correctly in the generated OpenAPI schema/Swagger UI — a raw
// multipart.Form body renders as generic placeholder field names there,
// which doesn't match what the handler actually expects.
type UploadForm struct {
	Files []huma.FormFile `form:"files" required:"true" doc:"Files to upload"`
}

type UploadFilesInput struct {
	ID      string `path:"id" doc:"Workspace ID"`
	Dir     string `query:"dir" doc:"Workspace-relative subdirectory to upload into (default: workspace root)"`
	RawBody huma.MultipartFormFiles[UploadForm]
}

type UploadFilesOutput struct {
	Body struct {
		WorkspaceID string      `json:"workspace_id"`
		Dir         string      `json:"dir"`
		Files       []FileEntry `json:"files"`
	}
}

func (s *server) uploadFilesHandler(ctx context.Context, input *UploadFilesInput) (*UploadFilesOutput, error) {
	if _, err := loadWorkspaceMeta(s.root, input.ID); err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	destDir, err := resolveWorkspacePath(s.root, input.ID, input.Dir, false)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	written := []FileEntry{}
	for _, fh := range input.RawBody.Data().Files {
		// filepath.Base strips any directory components a client might send
		// in the filename, so an upload can only ever land inside destDir.
		name := filepath.Base(fh.Filename)
		if name == "." || name == string(filepath.Separator) {
			return nil, huma.Error400BadRequest(fmt.Sprintf("invalid file name: %q", fh.Filename))
		}
		destPath := filepath.Join(destDir, name)
		dst, err := os.Create(destPath)
		if err != nil {
			return nil, huma.Error500InternalServerError(fmt.Sprintf("write %q: %v", name, err))
		}
		size, err := io.Copy(dst, fh)
		fh.Close()
		dst.Close()
		if err != nil {
			return nil, huma.Error500InternalServerError(fmt.Sprintf("write %q: %v", name, err))
		}
		relPath := filepath.ToSlash(filepath.Join(input.Dir, name))
		written = append(written, FileEntry{Path: relPath, Size: size})
	}

	resp := &UploadFilesOutput{}
	resp.Body.WorkspaceID = input.ID
	resp.Body.Dir = input.Dir
	resp.Body.Files = written
	return resp, nil
}

// --- download file ---

type DownloadFileInput struct {
	ID   string `path:"id" doc:"Workspace ID"`
	Path string `query:"path" required:"true" doc:"Workspace-relative path of the file to download"`
}

func (s *server) downloadFileHandler(ctx context.Context, input *DownloadFileInput) (*huma.StreamResponse, error) {
	resolved, err := resolveWorkspacePath(s.root, input.ID, input.Path, true)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}
	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() {
		return nil, huma.Error404NotFound(fmt.Sprintf("path is not a file: %s", input.Path))
	}

	contentType := mime.TypeByExtension(filepath.Ext(resolved))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return &huma.StreamResponse{
		Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", contentType)
			hctx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(resolved)))
			hctx.SetHeader("Content-Length", strconv.FormatInt(info.Size(), 10))
			f, err := os.Open(resolved)
			if err != nil {
				log.Printf("download: open %s: %v", resolved, err)
				return
			}
			defer f.Close()
			if _, err := io.Copy(hctx.BodyWriter(), f); err != nil {
				log.Printf("download: write %s: %v", resolved, err)
			}
		},
	}, nil
}

// --- download archive ---

type DownloadArchiveInput struct {
	ID  string `path:"id" doc:"Workspace ID"`
	Dir string `query:"dir" doc:"Workspace-relative subdirectory to archive (default: entire workspace)"`
}

func (s *server) downloadArchiveHandler(ctx context.Context, input *DownloadArchiveInput) (*huma.StreamResponse, error) {
	if _, err := resolveWorkspacePath(s.root, input.ID, input.Dir, true); err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}
	filesEntries, err := listWorkspaceFiles(s.root, input.ID, input.Dir)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	filesRoot := filesDir(s.root, input.ID)
	id := input.ID

	return &huma.StreamResponse{
		Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "application/zip")
			hctx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", id+".zip"))
			zw := zip.NewWriter(hctx.BodyWriter())
			defer zw.Close()
			for _, entry := range filesEntries {
				absPath := filepath.Join(filesRoot, filepath.FromSlash(entry.Path))
				zf, err := zw.Create(entry.Path)
				if err != nil {
					log.Printf("archive: create zip entry %s: %v", entry.Path, err)
					return
				}
				f, err := os.Open(absPath)
				if err != nil {
					log.Printf("archive: open %s: %v", absPath, err)
					return
				}
				_, err = io.Copy(zf, f)
				f.Close()
				if err != nil {
					log.Printf("archive: write %s: %v", entry.Path, err)
					return
				}
			}
		},
	}, nil
}
