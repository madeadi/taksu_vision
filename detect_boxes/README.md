# detect_boxes

YOLO bounding-box detection, as a standalone package. Given a loaded YOLO
model and a list of image paths, it runs inference and returns each
detection's axis-aligned `xyxy` box and ordered 4-corner `polygon` (oriented
for OBB models). No cropping — see `../crop_boxes` for that, and no shared
code with it (this package has its own copy of anything it needs).

## Setup

Requires Python 3.11 or 3.12.

```bash
python3.11 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

## API

Defined in `detector.py` (there is no package `__init__.py`):

| Name | Description |
| --- | --- |
| `detect` | Runs YOLO inference over a list of images and returns detected box entries (see below) |
| `blur_score` | Laplacian-variance blur score for an image; lower is blurrier |
| `order_quad_points` | Orders a quad's corners as top-left, top-right, bottom-right, bottom-left |
| `axis_aligned_envelope` | Axis-aligned `xyxy` envelope around an oriented quad's 4 corners |
| `IMG_EXTS` | Set of recognized image file extensions |

### `detect(model, image_paths, conf, blur_thresh=0.0, timing=None, device=None)`

1. **Load & optionally skip blurry frames** — reads each image with
   `cv2.imread`. If `blur_thresh > 0`, computes `blur_score` and skips the
   frame if it's below the threshold, adding it to `skipped` with
   `reason: "blurry"`.
2. **Run YOLO inference** — `model.predict(source=image, conf=conf,
   verbose=False, device=device)`. Branches on whether the model produced
   oriented boxes (`result.obb`) or plain axis-aligned boxes (`result.boxes`).
3. **For each detected box**, builds an entry with the box's axis-aligned
   `xyxy` and an ordered `polygon` — for OBB detections, the 4 corner points
   (`xyxyxyxy`) ordered via `order_quad_points`, with `axis_aligned_envelope`
   giving the `xyxy`; for plain detections, `polygon` is just the box's own 4
   corners.

Returns `(entries, skipped)`. Each `entries` item is `{"image", "image_name",
"box_index", "yolo_conf", "xyxy", "polygon", "is_obb"}`. `skipped` is a list
of `{"image", "image_name", "reason", ...}` dicts — one per whole image that
was never detected on (`"unreadable"` or `"blurry"`, with extra context for
the latter).

## HTTP API

`server.py` exposes `detect` as a FastAPI microservice ("Detect Boxes" in
Swagger UI). The model is loaded once at startup (via FastAPI's `lifespan`)
and stays warm across requests, avoiding per-call model-load / MPS
cold-start cost. Detection-only — no cropping, no barcode decoding, no OCR.

### Running

```bash
MODEL_PATH=weight.pt WORKSPACE_ROOT=/path/to/shared/storage uvicorn server:app --host 0.0.0.0 --port 8819
```

Swagger UI / interactive docs at `/docs` (OpenAPI schema at `/openapi.json`).

### Environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `MODEL_PATH` | `weight.pt` | Path to the YOLO model weights to load at startup. |
| `DEVICE` | auto | Inference device (`cpu`, `mps`, `cuda:0`, ...). Unset auto-detects MPS on Apple Silicon, else CPU. |
| `WORKSPACE_ROOT` | — | Shared-disk directory workspaces live under (same value as `../core` and every other service). Required — the server refuses to start without it. |

### `GET /health`

Returns `{"status", "device"}` — useful for confirming which device the
model loaded on.

### `POST /tasks`

Reads every image in a directory inside the given workspace and returns each
image's detected boxes. `images_dir` and `json_output_path` are relative to
the workspace, resolved against `$WORKSPACE_ROOT/{workspace_id}/files/`.
Workspaces themselves are created (and files uploaded into them) via
`../core` (see `../core/README.md`).

| Query param | Required | Default | Description |
| --- | --- | --- | --- |
| `workspace_id` | yes | — | Workspace to read/write files in. |
| `images_dir` | no | `""` (workspace root) | Directory (relative to the workspace) of image files to detect. |
| `json_output_path` | no | — | Path (relative to the workspace) to write the response JSON to. If omitted, the response isn't written to disk — only returned in the HTTP response. |
| `confidence` | no | `0.25` | YOLO detection confidence threshold (0-1). Boxes scoring below this are discarded. |
| `blur_threshold` | no | `0.0` | Minimum Laplacian-variance sharpness score required to process the image. The whole image is skipped (counted in `n_skipped`) if its score falls below this. `0` (default) disables the check. |

Plus an optional JSON request body:

```jsonc
{"filenames": ["a.jpg", "b.jpg"]}  // file names within images_dir to restrict detection to;
                                    // omit the body (or pass null/[]) to process every image
                                    // file in images_dir
```

Response shape:

```jsonc
{
  "input": {
    "images_dir": "images",
    "filenames": null,  // or e.g. ["a.jpg", "b.jpg"]
    "image_count": 1,
    "json_output_path": "output.json",  // or null
    "confidence": 0.25,
    "blur_threshold": 0.0
  },
  "output": {
    "images": [
      {
        "image_name": "image.jpeg",
        "boxes": [
          {
            "box_index": 0,
            "yolo_conf": 0.8785,
            "xyxy": [1728.7, 865.5, 2120.4, 987.1],
            "polygon": [[...], [...], [...], [...]],
            "is_obb": true
          }
        ],
        "skip_reason": null
      }
    ],
    "n_skipped": 0
  },
  "start_at": "2026-08-21T09:35:11.714928+00:00",
  "finished_at": "2026-08-21T09:35:11.912391+00:00",
  "success": true
  // "error": "..."  // present only when success is false
}
```

Feed an image's `boxes` straight into `../crop_boxes`'s `POST /tasks` (each
box's `box_index`, `is_obb`, `xyxy`, `polygon` are exactly the fields that
service expects) to get rectified crop files. This same response body is
also written to `json_output_path` when it's provided; otherwise it's only
returned in the HTTP response, not saved to disk.

### `GET /weights` / `DELETE /weights/{name}`

A JSON-backed registry (`weights.json`, global to this service — not scoped
per workspace) of trained model weights, each `{"name", "description",
"path"}`. Entries are added automatically when a `/train` job succeeds
(`name` is the job id, `path` the absolute path to the written weights
file) — there's no register endpoint. `DELETE` removes the registry entry
only; the weights file on disk is left in place.
