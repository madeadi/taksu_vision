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
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"google.golang.org/genai"
)

type taskInput struct {
	ImagesDir      string   `json:"images_dir"`
	Filenames      []string `json:"filenames"`
	JSONOutputPath *string  `json:"json_output_path"`
	Model          string   `json:"model"`
	Prompt         string   `json:"prompt"`
	MaxConcurrency int      `json:"max_concurrency"`
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
	resp := &HealthOutput{}
	resp.Body.Status = "ok"
	resp.Body.Model = defaultModel
	resp.Body.SaveResultsDir = saveResultsDir
	return resp, nil
}

// --- OCR (POST /tasks) ---

type TasksInput struct {
	WorkspaceID    string `query:"workspace_id" required:"true" doc:"Workspace to read/write files in."`
	ImagesDir      string `query:"images_dir" required:"true" doc:"Directory (relative to the workspace) of already-cropped images to OCR."`
	JSONOutputPath string `query:"json_output_path" doc:"Path (relative to the workspace) to write the response JSON to. If omitted, falls back to the server's SAVE_RESULTS_DIR env var (also relative to the workspace)."`
	Model          string `query:"model" doc:"Gemini model to OCR with. If omitted, falls back to the server's GEMINI_MODEL env var."`
	Prompt         string `query:"prompt" doc:"Prompt sent alongside each image. If omitted, falls back to a built-in default OCR prompt."`
	MaxConcurrency int    `query:"max_concurrency" doc:"Max number of Gemini requests to run in parallel. If omitted (or 0), falls back to the server's MAX_CONCURRENCY env var."`
	Body           struct {
		Filenames []string `json:"filenames,omitempty" doc:"Optional list of file names (within images_dir) to restrict OCR to. If omitted, every image file in images_dir is processed."`
	}
}

type TasksOutput struct {
	Body taskResult
}

func tasksHandler(ctx context.Context, input *TasksInput) (*TasksOutput, error) {
	workspaceID := input.WorkspaceID
	imagesDir := input.ImagesDir
	jsonOutputPath := input.JSONOutputPath
	model := input.Model
	if model == "" {
		model = defaultModel
	}
	prompt := input.Prompt
	if prompt == "" {
		prompt = defaultPrompt
	}
	maxConcurrency := input.MaxConcurrency
	if maxConcurrency == 0 {
		maxConcurrency = defaultMaxConcurrency
	}
	filenames := input.Body.Filenames

	sourceDir, err := resolveWorkspacePath(workspaceRoot, workspaceID, imagesDir, true)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	workspaceFilesDir, err := resolveWorkspacePath(workspaceRoot, workspaceID, "", true)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	// json_output_path, when given, wins over the server's SAVE_RESULTS_DIR default.
	outputRelPath := jsonOutputPath
	if outputRelPath == "" {
		outputRelPath = saveResultsDir
	}
	outputPath, err := resolveWorkspacePath(workspaceRoot, workspaceID, outputRelPath, false)
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

	startAt := time.Now().UTC()
	results := ocrImages(ctx, geminiClient, imagePaths, model, prompt, maxConcurrency)
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
			ImagesDir:      imagesDir,
			Filenames:      filenames,
			JSONOutputPath: jsonOutputPathPtr,
			Model:          model,
			Prompt:         prompt,
			MaxConcurrency: maxConcurrency,
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

var (
	workspaceRoot         string
	defaultModel          string
	defaultMaxConcurrency int
	saveResultsDir        string
	geminiClient          *genai.Client
)

func main() {
	host := flag.String("host", "0.0.0.0", "address to listen on")
	port := flag.Int("port", 8820, "port to listen on")
	flag.Parse()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY (or GOOGLE_API_KEY) must be set")
	}

	defaultModel = os.Getenv("GEMINI_MODEL")
	if defaultModel == "" {
		defaultModel = "gemini-2.5-flash-lite"
	}

	defaultMaxConcurrency = 4
	if v := os.Getenv("MAX_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Fatalf("invalid MAX_CONCURRENCY: %v", err)
		}
		defaultMaxConcurrency = n
	}

	workspaceRoot = os.Getenv("WORKSPACE_ROOT")
	if workspaceRoot == "" {
		log.Fatal("WORKSPACE_ROOT must be set")
	}

	saveResultsDir = os.Getenv("SAVE_RESULTS_DIR")
	if saveResultsDir == "" {
		saveResultsDir = "gemini_ocr/output.json"
	}

	var err error
	geminiClient, err = genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		log.Fatalf("create gemini client: %v", err)
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

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Printf("gemini_ocr listening on %s (docs at /docs)", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
