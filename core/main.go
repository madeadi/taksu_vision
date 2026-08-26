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
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/cmd"
	"github.com/pocketbase/pocketbase/core"

	_ "vision.taksu.tech/core/migrations"
)

const defaultTTLHours = 24 * 7
const defaultSweepIntervalMinutes = 60
// TODO: revisit once request auth (e.g. JWT) is in front of this API — an
// open CORS policy is fine while every endpoint is unauthenticated local
// dev tooling, but not once these routes can act on a caller's behalf.
const defaultCorsAllowedOrigins = "*"

type server struct {
	root string
	ttl  time.Duration
	app  core.App
}

func main() {
	host := flag.String("host", "0.0.0.0", "address to listen on")
	port := flag.Int("port", 8824, "port to listen on")
	flag.Parse()

	// PocketBase's own SQLite data dir: deliberately NOT under
	// WORKSPACE_ROOT, since the sweep and other workspace logic treat every
	// entry there as a workspace directory.
	dbDir := os.Getenv("DB_DIR")
	if dbDir == "" {
		dbDir = "./pb_data"
	}

	pb := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dbDir})

	// `core superuser upsert email pass` creates/updates an admin account
	// for the bundled PocketBase dashboard at /_/ — registered explicitly
	// since we skip pb.Start()/pb.Execute()'s default command set below in
	// favor of our own --host/--port-driven serve flow.
	pb.RootCmd.AddCommand(cmd.NewSuperuserCommand(pb))
	if flag.NArg() > 0 {
		if err := pb.Execute(); err != nil {
			log.Fatal(err)
		}
		return
	}

	workspaceRoot := os.Getenv("WORKSPACE_ROOT")
	if workspaceRoot == "" {
		log.Fatal("WORKSPACE_ROOT must be set")
	}
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		log.Fatalf("create WORKSPACE_ROOT %s: %v", workspaceRoot, err)
	}

	ttl := time.Duration(envInt("TTL_HOURS", defaultTTLHours)) * time.Hour
	sweepInterval := time.Duration(envInt("SWEEP_INTERVAL_MINUTES", defaultSweepIntervalMinutes)) * time.Minute

	if err := pb.Bootstrap(); err != nil {
		log.Fatalf("bootstrap pocketbase: %v", err)
	}
	if err := pb.RunAppMigrations(); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	srv := &server{root: workspaceRoot, ttl: ttl, app: pb}

	go startSweep(pb, workspaceRoot, sweepInterval, ttl)
	go startHealthCheck(pb)

	mux := http.NewServeMux()
	config := huma.DefaultConfig("Taksu Vision Core", "1.0.0")
	config.Info.Description = "Workspace lifecycle: create, upload into, download from, list, and delete workspaces on shared disk."
	// Stoplight Elements (huma's default /docs renderer) doesn't support a
	// "Try it" file picker for array-of-files multipart fields like our
	// upload-files operation's `files` field; Swagger UI does.
	config.DocsRenderer = huma.DocsRendererSwaggerUI
	api := humago.New(mux, config)
	srv.registerRoutes(api)
	mux.HandleFunc("/", indexHandler(otherServices()))

	// Mount the huma-built mux (health/docs/workspaces/files) as a fallback
	// under PocketBase's router, alongside PocketBase's own /api/* and /_/*
	// routes (admin UI + auto REST API), which take precedence since they're
	// more specific patterns.
	pb.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.Router.Any("/{path...}", apis.WrapStdHandler(mux))
		return se.Next()
	})

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Printf("core listening on %s (WORKSPACE_ROOT=%s, ttl=%s, db=%s, docs at /docs)", addr, workspaceRoot, ttl, dbDir)
	if err := apis.Serve(pb, apis.ServeConfig{
		HttpAddr:        addr,
		AllowedOrigins:  corsAllowedOrigins(),
		ShowStartBanner: false,
	}); err != nil {
		log.Fatal(err)
	}
}

// corsAllowedOrigins reads CORS_ALLOWED_ORIGINS as a comma-separated list of
// origins allowed to call this API cross-origin (e.g. the workspace
// management UI's dev/prod origin). Defaults to the Vite dev server origin.
func corsAllowedOrigins() []string {
	v := os.Getenv("CORS_ALLOWED_ORIGINS")
	if v == "" {
		v = defaultCorsAllowedOrigins
	}
	var origins []string
	for _, o := range strings.Split(v, ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}
	return origins
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

// service describes one other pipeline service for the index page. DocsURL
// is empty for a service with no generated OpenAPI docs — the index page
// falls back to linking its health check instead. Every current service
// has docs (huma for the Go ones, FastAPI for the Python ones), but the
// fallback stays in place for any future service that doesn't.
type service struct {
	Name    string
	BaseURL string
	DocsURL string
}

// otherServices reads each pipeline service's base URL from an env var
// (SPLIT_PDF_URL, DETECT_BOXES_URL, ...), defaulting to the localhost ports
// scripts/run_pipeline.sh starts them on. Override these when the services
// aren't all on localhost (e.g. one per container).
func otherServices() []service {
	svc := func(name, envVar, defaultURL string, hasDocs bool) service {
		base := os.Getenv(envVar)
		if base == "" {
			base = defaultURL
		}
		s := service{Name: name, BaseURL: base}
		if hasDocs {
			s.DocsURL = base + "/docs"
		}
		return s
	}
	return []service{
		svc("split_pdf", "SPLIT_PDF_URL", "http://localhost:8823", true),
		svc("detect_boxes", "DETECT_BOXES_URL", "http://localhost:8821", true),
		svc("crop_boxes", "CROP_BOXES_URL", "http://localhost:8822", true),
		svc("read_qr_ocr", "READ_QR_OCR_URL", "http://localhost:8819", true),
		svc("gemini_ocr", "GEMINI_OCR_URL", "http://localhost:8820", true),
	}
}

const indexHTMLTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Taksu Vision Core</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 32rem; margin: 4rem auto; line-height: 1.5; color: #1a1a1a; }
h1 { font-size: 1.25rem; }
h2 { font-size: 1rem; margin-top: 2rem; }
a { color: #2563eb; }
ul { padding-left: 1.25rem; }
</style>
</head>
<body>
<h1>Taksu Vision Core</h1>
<p>Workspace lifecycle service. Links to every pipeline service's API docs below.</p>

<h2>core (this service)</h2>
<ul>
<li><a href="/docs">API docs (Swagger UI)</a></li>
<li><a href="/openapi.json">OpenAPI spec</a></li>
<li><a href="/health">Health check</a></li>
</ul>

<h2>Pipeline services</h2>
<ul>
%s</ul>
</body>
</html>
`

func indexHandler(services []service) http.HandlerFunc {
	var items strings.Builder
	for _, s := range services {
		if s.DocsURL != "" {
			fmt.Fprintf(&items, "<li><strong>%s</strong> — <a href=\"%s\">API docs</a></li>\n", s.Name, s.DocsURL)
		} else {
			fmt.Fprintf(&items, "<li><strong>%s</strong> — no OpenAPI docs (plain <code>POST /tasks</code>); <a href=\"%s/health\">health</a></li>\n", s.Name, s.BaseURL)
		}
	}
	page := fmt.Sprintf(indexHTMLTemplate, items.String())
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	}
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
		OperationID: "list-workspaces",
		Method:      http.MethodGet,
		Path:        "/workspaces",
		Summary:     "List every workspace",
		Tags:        []string{"workspaces"},
	}, s.listWorkspacesHandler)

	huma.Register(api, huma.Operation{
		OperationID: "get-workspace",
		Method:      http.MethodGet,
		Path:        "/workspaces/{id}",
		Summary:     "Get workspace metadata and its file listing",
		Tags:        []string{"workspaces"},
	}, s.getWorkspaceHandler)

	huma.Register(api, huma.Operation{
		OperationID: "rename-workspace",
		Method:      http.MethodPatch,
		Path:        "/workspaces/{id}",
		Summary:     "Rename a workspace",
		Tags:        []string{"workspaces"},
	}, s.renameWorkspaceHandler)

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

	huma.Register(api, huma.Operation{
		OperationID: "create-service",
		Method:      http.MethodPost,
		Path:        "/services",
		Summary:     "Register a service to monitor",
		Tags:        []string{"services"},
	}, s.createServiceHandler)

	huma.Register(api, huma.Operation{
		OperationID: "list-services",
		Method:      http.MethodGet,
		Path:        "/services",
		Summary:     "List every monitored service",
		Tags:        []string{"services"},
	}, s.listServicesHandler)

	huma.Register(api, huma.Operation{
		OperationID: "update-service",
		Method:      http.MethodPatch,
		Path:        "/services/{id}",
		Summary:     "Edit a monitored service's registration",
		Tags:        []string{"services"},
	}, s.updateServiceHandler)

	huma.Register(api, huma.Operation{
		OperationID: "delete-service",
		Method:      http.MethodDelete,
		Path:        "/services/{id}",
		Summary:     "Stop monitoring a service",
		Tags:        []string{"services"},
	}, s.deleteServiceHandler)
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
	metas, err := listWorkspaces(s.app)
	count := 0
	if err == nil {
		count = len(metas)
	}
	resp := &HealthOutput{}
	resp.Body.Status = "ok"
	resp.Body.WorkspaceRoot = s.root
	resp.Body.WorkspaceCount = count
	return resp, nil
}

// --- create workspace ---

type CreateWorkspaceInput struct {
	Body struct {
		Name string `json:"name,omitempty" doc:"Display name (default: derived from the generated workspace id)" example:"Q3 invoices"`
	}
}

type CreateWorkspaceOutput struct {
	Status int
	Body   struct {
		WorkspaceID string `json:"workspace_id"`
		Name        string `json:"name"`
		CreatedAt   string `json:"created_at"`
		ExpiresAt   string `json:"expires_at"`
	}
}

func (s *server) createWorkspaceHandler(ctx context.Context, input *CreateWorkspaceInput) (*CreateWorkspaceOutput, error) {
	meta, err := createWorkspace(s.app, s.root, s.ttl, input.Body.Name)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &CreateWorkspaceOutput{Status: http.StatusCreated}
	resp.Body.WorkspaceID = meta.WorkspaceID
	resp.Body.Name = meta.Name
	resp.Body.CreatedAt = meta.CreatedAt.Format(time.RFC3339Nano)
	resp.Body.ExpiresAt = meta.ExpiresAt.Format(time.RFC3339Nano)
	return resp, nil
}

// --- rename workspace ---

type RenameWorkspaceInput struct {
	ID   string `path:"id" doc:"Workspace ID"`
	Body struct {
		Name string `json:"name" required:"true" doc:"New display name" example:"Q3 invoices"`
	}
}

type RenameWorkspaceOutput struct {
	Body struct {
		WorkspaceID string `json:"workspace_id"`
		Name        string `json:"name"`
		CreatedAt   string `json:"created_at"`
		ExpiresAt   string `json:"expires_at"`
	}
}

func (s *server) renameWorkspaceHandler(ctx context.Context, input *RenameWorkspaceInput) (*RenameWorkspaceOutput, error) {
	meta, err := renameWorkspace(s.app, input.ID, input.Body.Name)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}
	resp := &RenameWorkspaceOutput{}
	resp.Body.WorkspaceID = meta.WorkspaceID
	resp.Body.Name = meta.Name
	resp.Body.CreatedAt = meta.CreatedAt.Format(time.RFC3339Nano)
	resp.Body.ExpiresAt = meta.ExpiresAt.Format(time.RFC3339Nano)
	return resp, nil
}

// --- list workspaces ---

type WorkspaceSummary struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at"`
}

type ListWorkspacesOutput struct {
	Body struct {
		Workspaces []WorkspaceSummary `json:"workspaces"`
	}
}

func (s *server) listWorkspacesHandler(ctx context.Context, input *struct{}) (*ListWorkspacesOutput, error) {
	metas, err := listWorkspaces(s.app)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &ListWorkspacesOutput{}
	resp.Body.Workspaces = make([]WorkspaceSummary, 0, len(metas))
	for _, meta := range metas {
		resp.Body.Workspaces = append(resp.Body.Workspaces, WorkspaceSummary{
			WorkspaceID: meta.WorkspaceID,
			Name:        meta.Name,
			CreatedAt:   meta.CreatedAt.Format(time.RFC3339Nano),
			ExpiresAt:   meta.ExpiresAt.Format(time.RFC3339Nano),
		})
	}
	return resp, nil
}

// --- get workspace ---

type WorkspaceIDInput struct {
	ID string `path:"id" doc:"Workspace ID"`
}

type GetWorkspaceOutput struct {
	Body struct {
		WorkspaceID string      `json:"workspace_id"`
		Name        string      `json:"name"`
		CreatedAt   string      `json:"created_at"`
		ExpiresAt   string      `json:"expires_at"`
		Files       []FileEntry `json:"files"`
	}
}

func (s *server) getWorkspaceHandler(ctx context.Context, input *WorkspaceIDInput) (*GetWorkspaceOutput, error) {
	meta, err := loadWorkspaceMeta(s.app, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}
	files, err := listWorkspaceFiles(s.root, input.ID, "")
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &GetWorkspaceOutput{}
	resp.Body.WorkspaceID = meta.WorkspaceID
	resp.Body.Name = meta.Name
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
	if err := deleteWorkspace(s.app, s.root, input.ID); err != nil {
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
	if _, err := loadWorkspaceMeta(s.app, input.ID); err != nil {
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

// --- services ---

type ServiceSummary struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	URL        string  `json:"url"`
	WebURL     string  `json:"web_url,omitempty"`
	Online     bool    `json:"online"`
	LastSeenAt *string `json:"last_seen_at,omitempty" doc:"RFC3339 timestamp of the last successful health check; absent if never seen online"`
}

func serviceSummary(meta ServiceMeta) ServiceSummary {
	summary := ServiceSummary{
		ID:     meta.ID,
		Name:   meta.Name,
		URL:    meta.URL,
		WebURL: meta.WebURL,
		Online: meta.Online,
	}
	if !meta.LastSeenAt.IsZero() {
		formatted := meta.LastSeenAt.Format(time.RFC3339Nano)
		summary.LastSeenAt = &formatted
	}
	return summary
}

type CreateServiceInput struct {
	Body struct {
		Name   string `json:"name" required:"true" doc:"Display name" example:"split_pdf"`
		URL    string `json:"url" required:"true" doc:"Base URL, health-checked at {url}/health" example:"http://localhost:8823"`
		WebURL string `json:"web_url,omitempty" doc:"Web app base URL, iframed by core_ui" example:"http://localhost:8825"`
	}
}

type CreateServiceOutput struct {
	Status int
	Body   ServiceSummary
}

func (s *server) createServiceHandler(ctx context.Context, input *CreateServiceInput) (*CreateServiceOutput, error) {
	meta, err := createService(s.app, input.Body.Name, input.Body.URL, input.Body.WebURL)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &CreateServiceOutput{Status: http.StatusCreated, Body: serviceSummary(meta)}, nil
}

type UpdateServiceInput struct {
	ID   string `path:"id" doc:"Service ID"`
	Body struct {
		Name   string `json:"name" required:"true" doc:"Display name" example:"split_pdf"`
		URL    string `json:"url" required:"true" doc:"Base URL, health-checked at {url}/health" example:"http://localhost:8823"`
		WebURL string `json:"web_url,omitempty" doc:"Web app base URL, iframed by core_ui" example:"http://localhost:8825"`
	}
}

type UpdateServiceOutput struct {
	Body ServiceSummary
}

func (s *server) updateServiceHandler(ctx context.Context, input *UpdateServiceInput) (*UpdateServiceOutput, error) {
	meta, err := updateService(s.app, input.ID, input.Body.Name, input.Body.URL, input.Body.WebURL)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}
	return &UpdateServiceOutput{Body: serviceSummary(meta)}, nil
}

type ListServicesOutput struct {
	Body struct {
		Services []ServiceSummary `json:"services"`
	}
}

func (s *server) listServicesHandler(ctx context.Context, input *struct{}) (*ListServicesOutput, error) {
	metas, err := listServices(s.app)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &ListServicesOutput{}
	resp.Body.Services = make([]ServiceSummary, 0, len(metas))
	for _, meta := range metas {
		resp.Body.Services = append(resp.Body.Services, serviceSummary(meta))
	}
	return resp, nil
}

type ServiceIDInput struct {
	ID string `path:"id" doc:"Service ID"`
}

type DeleteServiceOutput struct {
	Body struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
	}
}

func (s *server) deleteServiceHandler(ctx context.Context, input *ServiceIDInput) (*DeleteServiceOutput, error) {
	if err := deleteService(s.app, input.ID); err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}
	resp := &DeleteServiceOutput{}
	resp.Body.ID = input.ID
	resp.Body.Deleted = true
	return resp, nil
}
