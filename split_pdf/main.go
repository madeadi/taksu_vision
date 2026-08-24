// split_pdf HTTP microservice: splits a server-local PDF into single-page
// PDF files. Stateless — no model to keep warm. Mirrors the request/response
// conventions used by ../crop_boxes and ../detect_boxes (GET /health,
// POST /tasks with query-param paths and a JSON envelope response), but as a
// plain net/http server since this service is Go, not Python/FastAPI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
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

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func tasksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	workspaceID := query.Get("workspace_id")
	pdfPath := query.Get("pdf_path")
	pagesOutDir := query.Get("pages_out_dir")
	jsonOutputPath := query.Get("json_output_path")

	if workspaceID == "" {
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}
	if pdfPath == "" {
		http.Error(w, "pdf_path is required", http.StatusBadRequest)
		return
	}
	if pagesOutDir == "" {
		http.Error(w, "pages_out_dir is required", http.StatusBadRequest)
		return
	}

	resolvedPDFPath, err := resolveWorkspacePath(workspaceRoot, workspaceID, pdfPath, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resolvedPagesOutDir, err := resolveWorkspacePath(workspaceRoot, workspaceID, pagesOutDir, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var resolvedJSONOutputPath string
	if jsonOutputPath != "" {
		resolvedJSONOutputPath, err = resolveWorkspacePath(workspaceRoot, workspaceID, jsonOutputPath, false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
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

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/tasks", tasksHandler)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Printf("split_pdf listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
