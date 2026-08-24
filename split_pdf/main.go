// split_pdf HTTP microservice: splits a server-local PDF into single-page
// PDF files. Stateless — no model to keep warm. Mirrors the request/response
// conventions used by ../crop_boxes and ../detect_boxes (GET /health,
// POST /tasks with query-param paths and a JSON envelope response). Routed
// through huma (https://huma.rocks), like ../core, so the API is
// self-documenting: GET /docs (Swagger UI) and GET /openapi.json.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type taskInput struct {
	WorkspaceID    string `json:"workspace_id"`
	PDFPath        string `json:"pdf_path"`
	PDFName        string `json:"pdf_name"`
	PagesOutDir    string `json:"pages_out_dir"`
	JSONOutputPath string `json:"json_output_path,omitempty"`
}

type taskOutput struct {
	Pages     []PageResult `json:"pages"`
	PagesDir  string       `json:"pages_dir"`
	PageCount int          `json:"page_count"`
}

type taskResult struct {
	Input      taskInput  `json:"input"`
	Output     taskOutput `json:"output"`
	StartAt    string     `json:"start_at"`
	FinishedAt string     `json:"finished_at"`
	Success    bool       `json:"success"`
	Error      string     `json:"error,omitempty"`
}

// --- health ---

type HealthOutput struct {
	Body struct {
		Status string `json:"status" example:"ok"`
	}
}

func healthHandler(ctx context.Context, input *struct{}) (*HealthOutput, error) {
	resp := &HealthOutput{}
	resp.Body.Status = "ok"
	return resp, nil
}

// --- split (POST /tasks) ---

type TasksInput struct {
	WorkspaceID    string `query:"workspace_id" required:"true" doc:"Workspace to read/write files in."`
	PDFPath        string `query:"pdf_path" required:"true" doc:"Path (relative to the workspace) of the PDF to split."`
	PagesOutDir    string `query:"pages_out_dir" required:"true" doc:"Directory (relative to the workspace) to write single-page PDFs into. Created (with parents) if it doesn't exist."`
	JSONOutputPath string `query:"json_output_path" doc:"Path (relative to the workspace) to write the response JSON to. If omitted, the response isn't written to disk."`
}

type TasksOutput struct {
	Body taskResult
}

func tasksHandler(ctx context.Context, input *TasksInput) (*TasksOutput, error) {
	workspaceID := input.WorkspaceID
	pdfPath := input.PDFPath
	pagesOutDir := input.PagesOutDir
	jsonOutputPath := input.JSONOutputPath

	resolvedPDFPath, err := resolveWorkspacePath(workspaceRoot, workspaceID, pdfPath, true)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	resolvedPagesOutDir, err := resolveWorkspacePath(workspaceRoot, workspaceID, pagesOutDir, false)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	var resolvedJSONOutputPath string
	if jsonOutputPath != "" {
		resolvedJSONOutputPath, err = resolveWorkspacePath(workspaceRoot, workspaceID, jsonOutputPath, false)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
	}

	startAt := time.Now().UTC()
	pages, splitErr := splitPDF(resolvedPDFPath, resolvedPagesOutDir)
	finishedAt := time.Now().UTC()

	// Rewrite absolute on-disk paths back to workspace-relative before
	// building the response, so a caller can feed page_path straight into
	// the next service's /tasks call. Resolved (symlinks followed) the same
	// way resolveWorkspacePath resolved resolvedPagesOutDir above, so
	// filepath.Rel below compares paths from the same realm (matters if
	// WORKSPACE_ROOT sits behind a symlink, e.g. macOS's /tmp -> /private/tmp).
	filesRoot, err := resolveWorkspacePath(workspaceRoot, workspaceID, "", false)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	for i := range pages {
		if rel, err := filepath.Rel(filesRoot, pages[i].PagePath); err == nil {
			pages[i].PagePath = rel
		}
	}

	result := taskResult{
		Input: taskInput{
			WorkspaceID:    workspaceID,
			PDFPath:        pdfPath,
			PDFName:        filepath.Base(pdfPath),
			PagesOutDir:    pagesOutDir,
			JSONOutputPath: jsonOutputPath,
		},
		Output: taskOutput{
			Pages:     pages,
			PagesDir:  pagesOutDir,
			PageCount: len(pages),
		},
		StartAt:    startAt.Format(time.RFC3339Nano),
		FinishedAt: finishedAt.Format(time.RFC3339Nano),
		Success:    splitErr == nil,
	}
	if splitErr != nil {
		result.Error = splitErr.Error()
	}

	if resolvedJSONOutputPath != "" {
		if err := writeJSONFile(resolvedJSONOutputPath, result); err != nil {
			log.Printf("failed to write json_output_path %s: %v", jsonOutputPath, err)
		}
	}

	resp := &TasksOutput{Body: result}
	return resp, nil
}

func writeJSONFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

var workspaceRoot string

func main() {
	host := flag.String("host", "0.0.0.0", "address to listen on")
	port := flag.Int("port", 8823, "port to listen on")
	flag.Parse()

	workspaceRoot = os.Getenv("WORKSPACE_ROOT")
	if workspaceRoot == "" {
		log.Fatal("WORKSPACE_ROOT must be set")
	}

	mux := http.NewServeMux()
	config := huma.DefaultConfig("Taksu Vision split_pdf", "1.0.0")
	config.Info.Description = "Splits a workspace-relative PDF into single-page PDF files."
	// Match ../core's docs renderer so /docs looks and behaves the same
	// across every huma-based service in this repo.
	config.DocsRenderer = huma.DocsRendererSwaggerUI
	api := humago.New(mux, config)

	huma.Register(api, huma.Operation{
		OperationID: "health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Tags:        []string{"health"},
	}, healthHandler)

	huma.Register(api, huma.Operation{
		OperationID: "split-pdf",
		Method:      http.MethodPost,
		Path:        "/tasks",
		Summary:     "Split a PDF into single-page PDFs",
		Tags:        []string{"tasks"},
	}, tasksHandler)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Printf("split_pdf listening on %s (docs at /docs)", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
