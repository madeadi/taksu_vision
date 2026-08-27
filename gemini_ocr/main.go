// gemini_ocr HTTP microservice: Gemini-based OCR for already-cropped images.
//
// Takes already-detected, already-straightened crop image files as input
// (e.g. the crops_dir written by ../crop_boxes's POST /tasks) and asks
// Gemini to read the text out of each one. No detection/rectification of
// its own — see ../crop_boxes for that. There's no model to keep warm (the
// Gemini client just wraps HTTP calls to Google's API), so there's no
// lifespan/startup load here. Routed through huma (https://huma.rocks),
// like ../core, so the API is self-documenting: GET /docs (Swagger UI) and
// GET /openapi.json.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type taskInput struct {
	ImagesDir       string         `json:"images_dir"`
	Filenames       []string       `json:"filenames"`
	JSONOutputPath  *string        `json:"json_output_path"`
	Model           string         `json:"model"`
	Instruction     string         `json:"instruction"`
	FormattedOutput map[string]any `json:"formatted_output"`
	Patterns        []string       `json:"patterns"`
	RemovePatterns  []string       `json:"remove_patterns"`
	MaxConcurrency  int            `json:"max_concurrency"`
}

type taskOutput struct {
	Results           []OCRResult `json:"results"`
	NProcessed        int         `json:"n_processed"`
	NFailed           int         `json:"n_failed"`
	TotalInputTokens  int32       `json:"total_input_tokens"`
	TotalOutputTokens int32       `json:"total_output_tokens"`
	TotalCostUSD      *float64    `json:"total_cost_usd"`
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
		Status         string `json:"status" example:"ok"`
		Model          string `json:"model"`
		SaveResultsDir string `json:"save_results_dir"`
	}
}

func healthHandler(ctx context.Context, input *struct{}) (*HealthOutput, error) {
	configMu.RLock()
	defer configMu.RUnlock()
	resp := &HealthOutput{}
	resp.Body.Status = "ok"
	resp.Body.Model = defaultModel
	resp.Body.SaveResultsDir = saveResultsDir
	return resp, nil
}

// --- config refresh (POST /config/refresh) ---

type RefreshConfigOutput struct {
	Body struct {
		Success        bool   `json:"success"`
		Model          string `json:"model"`
		MaxConcurrency int    `json:"max_concurrency"`
		WorkspaceRoot  string `json:"workspace_root"`
		SaveResultsDir string `json:"save_results_dir"`
	}
}

func refreshConfigHandler(ctx context.Context, input *struct{}) (*RefreshConfigOutput, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read config: " + err.Error())
	}
	if err := applyConfig(cfg); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	configMu.RLock()
	defer configMu.RUnlock()
	resp := &RefreshConfigOutput{}
	resp.Body.Success = true
	resp.Body.Model = defaultModel
	resp.Body.MaxConcurrency = defaultMaxConcurrency
	resp.Body.WorkspaceRoot = workspaceRoot
	resp.Body.SaveResultsDir = saveResultsDir
	return resp, nil
}

// --- logs (GET /logs) ---

type LogsOutput struct {
	Body struct {
		Lines []string `json:"lines"`
	}
}

func logsHandler(ctx context.Context, input *struct{}) (*LogsOutput, error) {
	resp := &LogsOutput{}
	resp.Body.Lines = logRing.Lines()
	return resp, nil
}

// --- OCR (POST /tasks) ---

type TasksInput struct {
	WorkspaceID    string `query:"workspace_id" required:"true" doc:"Workspace to read/write files in."`
	ImagesDir      string `query:"images_dir" required:"true" doc:"Directory (relative to the workspace) of already-cropped images to OCR."`
	JSONOutputPath string `query:"json_output_path" doc:"Path (relative to the workspace) to write the response JSON to. If omitted, falls back to the server's SAVE_RESULTS_DIR env var (also relative to the workspace)."`
	Model          string `query:"model" doc:"Gemini model to OCR with. If omitted, falls back to the server's GEMINI_MODEL env var."`
	MaxConcurrency int    `query:"max_concurrency" doc:"Max number of Gemini requests to run in parallel. If omitted (or 0), falls back to the server's MAX_CONCURRENCY env var."`
	Body           struct {
		Filenames       []string       `json:"filenames,omitempty" doc:"Optional list of file names (within images_dir) to restrict OCR to. If omitted, every image file in images_dir is processed."`
		Instruction     string         `json:"instruction,omitempty" doc:"Free-text instruction describing what to extract from each image. If omitted, falls back to a built-in general-OCR instruction."`
		FormattedOutput map[string]any `json:"formatted_output,omitempty" doc:"Example JSON object showing the shape/keys/sample values you want populated in each result's formatted_output — an example, not a formal schema. If omitted, each result's formatted_output is {}."`
		Patterns        []string       `json:"patterns,omitempty" doc:"Text pattern descriptions (e.g. '16-digit NIK number') to check the detected text against. Each result's matched_patterns echoes back the exact strings from this list that matched. If omitted, matched_patterns is []."`
		RemovePatterns  []string       `json:"remove_patterns,omitempty" doc:"Text patterns to strip out of the detected text before checking it against patterns. If omitted, no filtering is applied."`
	}
}

type TasksOutput struct {
	Body taskResult
}

func tasksHandler(ctx context.Context, input *TasksInput) (*TasksOutput, error) {
	configMu.RLock()
	currentWorkspaceRoot := workspaceRoot
	currentSaveResultsDir := saveResultsDir
	currentGeminiClient := geminiClient
	model := input.Model
	if model == "" {
		model = defaultModel
	}
	maxConcurrency := input.MaxConcurrency
	if maxConcurrency == 0 {
		maxConcurrency = defaultMaxConcurrency
	}
	configMu.RUnlock()

	workspaceID := input.WorkspaceID
	imagesDir := input.ImagesDir
	jsonOutputPath := input.JSONOutputPath
	instruction := input.Body.Instruction
	if instruction == "" {
		instruction = defaultInstruction
	}
	formattedOutput := input.Body.FormattedOutput
	patterns := input.Body.Patterns
	removePatterns := input.Body.RemovePatterns
	filenames := input.Body.Filenames

	sourceDir, err := resolveWorkspacePath(currentWorkspaceRoot, workspaceID, imagesDir, true)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	workspaceFilesDir, err := resolveWorkspacePath(currentWorkspaceRoot, workspaceID, "", true)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	// json_output_path, when given, wins over the server's SAVE_RESULTS_DIR default.
	outputRelPath := jsonOutputPath
	if outputRelPath == "" {
		outputRelPath = currentSaveResultsDir
	}
	outputPath, err := resolveWorkspacePath(currentWorkspaceRoot, workspaceID, outputRelPath, false)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	info, err := os.Stat(sourceDir)
	if err != nil || !info.IsDir() {
		return nil, huma.Error400BadRequest(fmt.Sprintf("images_dir not found or not a directory: %q", imagesDir))
	}

	var imagePaths []string
	if len(filenames) > 0 {
		for _, name := range filenames {
			candidate := filepath.Join(sourceDir, name)
			// parent must be exactly sourceDir: rejects path separators/traversal (e.g. "../x").
			fi, statErr := os.Stat(candidate)
			if filepath.Dir(candidate) != sourceDir || statErr != nil || fi.IsDir() {
				return nil, huma.Error400BadRequest(fmt.Sprintf("filenames entry not found in images_dir: %q", name))
			}
			imagePaths = append(imagePaths, candidate)
		}
	} else {
		entries, err := os.ReadDir(sourceDir)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if imgExts[strings.ToLower(filepath.Ext(e.Name()))] {
				imagePaths = append(imagePaths, filepath.Join(sourceDir, e.Name()))
			}
		}
		sort.Strings(imagePaths)
	}
	if len(imagePaths) == 0 {
		return nil, huma.Error400BadRequest(fmt.Sprintf("No image files found in images_dir: %q", imagesDir))
	}

	var jsonOutputPathPtr *string
	if jsonOutputPath != "" {
		jsonOutputPathPtr = &jsonOutputPath
	}

	prompt := buildPrompt(ExtractionConfig{
		Instruction:     instruction,
		FormattedOutput: formattedOutput,
		Patterns:        patterns,
		RemovePatterns:  removePatterns,
	})

	startAt := time.Now().UTC()
	results := ocrImages(ctx, currentGeminiClient, imagePaths, model, prompt, maxConcurrency)
	finishedAt := time.Now().UTC()

	var totalInputTokens, totalOutputTokens int32
	var totalCostUSD float64
	var haveCost bool
	nProcessed, nFailed := 0, 0
	for i := range results {
		if rel, err := filepath.Rel(workspaceFilesDir, results[i].Image); err == nil {
			results[i].Image = rel
		}
		if results[i].Error == nil {
			nProcessed++
		} else {
			nFailed++
		}
		if results[i].InputTokens != nil {
			totalInputTokens += *results[i].InputTokens
		}
		if results[i].OutputTokens != nil {
			totalOutputTokens += *results[i].OutputTokens
		}
		if results[i].CostUSD != nil {
			totalCostUSD += *results[i].CostUSD
			haveCost = true
		}
	}
	var totalCostUSDPtr *float64
	if haveCost {
		totalCostUSDPtr = &totalCostUSD
	}

	result := taskResult{
		Input: taskInput{
			ImagesDir:       imagesDir,
			Filenames:       filenames,
			JSONOutputPath:  jsonOutputPathPtr,
			Model:           model,
			Instruction:     instruction,
			FormattedOutput: formattedOutput,
			Patterns:        patterns,
			RemovePatterns:  removePatterns,
			MaxConcurrency:  maxConcurrency,
		},
		Output: taskOutput{
			Results:           results,
			NProcessed:        nProcessed,
			NFailed:           nFailed,
			TotalInputTokens:  totalInputTokens,
			TotalOutputTokens: totalOutputTokens,
			TotalCostUSD:      totalCostUSDPtr,
		},
		StartAt:    startAt.Format(time.RFC3339Nano),
		FinishedAt: finishedAt.Format(time.RFC3339Nano),
		Success:    true,
	}

	if err := writeJSONFile(outputPath, result); err != nil {
		log.Printf("failed to write json_output_path %s: %v", outputRelPath, err)
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

func main() {
	host := flag.String("host", "0.0.0.0", "address to listen on")
	port := flag.Int("port", 8820, "port to listen on")
	webDir := flag.String("web-dir", "web/dist", "path to the built web app (cd web && npm run build); if missing, /web is not served")
	flag.StringVar(&configPath, "config", "config.yaml", "path to YAML config file (optional; any field left unset falls back to its env var, then a hardcoded default). Re-read at runtime via POST /config/refresh.")
	flag.Parse()

	logRing = newLogRingWriter(logBufferCapacity)
	log.SetOutput(io.MultiWriter(os.Stderr, logRing))

	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("load config %s: %v", configPath, err)
	}
	if err := applyConfig(cfg); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	config := huma.DefaultConfig("Gemini OCR", "1.0.0")
	config.Info.Description = fmt.Sprintf(
		"OCR for already-cropped images via the Gemini API (default model %q, "+
			"override with the `model` query param or the GEMINI_MODEL env var). "+
			"Point POST /tasks at a workspace-relative folder of crop files "+
			"(e.g. from ../crop_boxes) to get back each image's path and recognized text.",
		defaultModel,
	)
	api := humago.New(mux, config)

	huma.Register(api, huma.Operation{
		OperationID: "health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Tags:        []string{"health"},
	}, healthHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gemini-ocr",
		Method:      http.MethodPost,
		Path:        "/tasks",
		Summary:     "OCR a workspace directory of crop images with Gemini",
		Tags:        []string{"tasks"},
	}, tasksHandler)

	huma.Register(api, huma.Operation{
		OperationID: "config-refresh",
		Method:      http.MethodPost,
		Path:        "/config/refresh",
		Summary:     "Re-read config.yaml (and env vars) without restarting the server",
		Tags:        []string{"config"},
	}, refreshConfigHandler)

	huma.Register(api, huma.Operation{
		OperationID: "logs",
		Method:      http.MethodGet,
		Path:        "/logs",
		Summary:     "Recent in-memory log lines (see web app's Logs tab)",
		Tags:        []string{"logs"},
	}, logsHandler)

	// Serves the built web app (see web/README-equivalent section in
	// README.md) at /web on this same port/process, once it's been built
	// (`cd web && npm run build` -> web/dist; vite.config.ts sets
	// base: "/web/" for that build so its asset URLs match this mount
	// point). Registered last so it only catches paths no route above
	// matched. Absent in dev, where the web app instead runs its own
	// `npm run dev` server (see web/).
	if info, err := os.Stat(*webDir); err == nil && info.IsDir() {
		mux.Handle("/web/", http.StripPrefix("/web/", http.FileServer(http.Dir(*webDir))))
		log.Printf("serving web app from %s at /web", *webDir)
	} else {
		log.Printf("web app dist dir %s not found — /web not served (run `cd web && npm run build`, or use its dev server per README)", *webDir)
	}

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Printf("gemini_ocr listening on %s (docs at /docs)", addr)
	if err := http.ListenAndServe(addr, withCORS(mux, corsAllowedOrigins())); err != nil {
		log.Fatal(err)
	}
}
