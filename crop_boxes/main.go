// crop_boxes HTTP microservice: crop extraction from pre-detected boxes.
//
// Stateless — no model to keep warm, so there's no lifespan/startup step.
// Feed it the boxes from ../detect_boxes's response (or any source producing
// the same shape) and it returns padded, orientation-corrected crop files.
// Mirrors the request/response conventions used by ../split_pdf and
// ../detect_boxes (GET /health, POST /tasks with query-param paths and a
// JSON envelope response). Routed through huma (https://huma.rocks), like
// ../core, so the API is self-documenting: GET /docs (Swagger UI) and
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
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type taskInput struct {
	ImageName      string  `json:"image_name"`
	ImagePath      string  `json:"image_path"`
	PadRatio       float64 `json:"pad_ratio"`
	CropsOutDir    string  `json:"crops_out_dir"`
	JSONOutputPath *string `json:"json_output_path"`
	BoxCount       int     `json:"box_count"`
}

type taskOutput struct {
	Crops    []CropEntry `json:"crops"`
	CropsDir string      `json:"crops_dir"`
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

// --- crop (POST /tasks) ---

type TasksInput struct {
	WorkspaceID    string  `query:"workspace_id" required:"true" doc:"Workspace to read/write files in."`
	ImagePath      string  `query:"image_path" required:"true" doc:"Path (relative to the workspace) of the image to crop from."`
	PadRatio       float64 `query:"pad_ratio" default:"0.15" doc:"Fractional margin added around each box before cropping (0.15 = 15% padding)."`
	CropsOutDir    string  `query:"crops_out_dir" required:"true" doc:"Directory (relative to the workspace) to write crop files into. Created if it doesn't exist."`
	JSONOutputPath string  `query:"json_output_path" doc:"Path (relative to the workspace) to write the response JSON to. If omitted, the response isn't written to disk."`
	Body           []Box   `doc:"Boxes to crop: {box_index, is_obb, xyxy, polygon}. xyxy is required when is_obb is false; polygon (4 [x,y] corners, any order) is required when is_obb is true."`
}

type TasksOutput struct {
	Body taskResult
}

func tasksHandler(ctx context.Context, input *TasksInput) (*TasksOutput, error) {
	workspaceID := input.WorkspaceID
	imagePath := input.ImagePath
	padRatio := input.PadRatio
	cropsOutDir := input.CropsOutDir
	jsonOutputPath := input.JSONOutputPath
	boxes := input.Body

	resolvedImagePath, err := resolveWorkspacePath(workspaceRoot, workspaceID, imagePath, true)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	resolvedCropsOutDir, err := resolveWorkspacePath(workspaceRoot, workspaceID, cropsOutDir, false)
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

	for _, box := range boxes {
		if box.IsOBB && box.Polygon == nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("box_index %d: is_obb=true requires polygon", box.BoxIndex))
		}
		if !box.IsOBB && box.XYXY == nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("box_index %d: is_obb=false requires xyxy", box.BoxIndex))
		}
	}

	// Resolved (symlinks followed) the same way resolveWorkspacePath resolved
	// resolvedCropsOutDir above, so filepath.Rel below compares paths from the
	// same realm (matters if WORKSPACE_ROOT sits behind a symlink, e.g.
	// macOS's /tmp -> /private/tmp).
	filesRoot, err := resolveWorkspacePath(workspaceRoot, workspaceID, "", false)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	var jsonOutputPathPtr *string
	if jsonOutputPath != "" {
		jsonOutputPathPtr = &jsonOutputPath
	}

	startAt := time.Now().UTC()
	entries, cropErr := CropBoxes(resolvedImagePath, boxes, padRatio, resolvedCropsOutDir)
	finishedAt := time.Now().UTC()

	for i := range entries {
		if entries[i].CropPath != nil {
			if rel, err := filepath.Rel(filesRoot, *entries[i].CropPath); err == nil {
				entries[i].CropPath = &rel
			}
		}
	}
	if entries == nil {
		entries = []CropEntry{}
	}

	result := taskResult{
		Input: taskInput{
			ImageName:      filepath.Base(imagePath),
			ImagePath:      imagePath,
			PadRatio:       padRatio,
			CropsOutDir:    cropsOutDir,
			JSONOutputPath: jsonOutputPathPtr,
			BoxCount:       len(boxes),
		},
		Output: taskOutput{
			Crops:    entries,
			CropsDir: cropsOutDir,
		},
		StartAt:    startAt.Format(time.RFC3339Nano),
		FinishedAt: finishedAt.Format(time.RFC3339Nano),
		Success:    cropErr == nil,
	}
	if cropErr != nil {
		result.Error = cropErr.Error()
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
	port := flag.Int("port", 8822, "port to listen on")
	flag.Parse()

	workspaceRoot = os.Getenv("WORKSPACE_ROOT")
	if workspaceRoot == "" {
		log.Fatal("WORKSPACE_ROOT must be set")
	}

	mux := http.NewServeMux()
	config := huma.DefaultConfig("Taksu Vision crop_boxes", "1.0.0")
	config.Info.Description = "Crop extraction from pre-detected boxes — no YOLO model, just image geometry."
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
		OperationID: "crop-boxes",
		Method:      http.MethodPost,
		Path:        "/tasks",
		Summary:     "Crop boxes out of an image",
		Tags:        []string{"tasks"},
	}, tasksHandler)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Printf("crop_boxes listening on %s (docs at /docs)", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
