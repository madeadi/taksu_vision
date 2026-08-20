# image_crops

YOLO oriented-bounding-box (OBB) detection and crop rectification, as a
standalone package. Given a loaded YOLO model and a list of image paths, it
runs inference and turns each detection into an upright, padded crop ready
for downstream barcode decoding or OCR.

Consumed by `../pipeline_nuh` (which orchestrates this service together with
`../read_qr_ocr`) and by `../read_qr_ocr`, which reads the crop files this
service saves — but this package has no dependency on either of them, just
`numpy` and `cv2`.

## Setup

Requires Python 3.11 or 3.12.

```bash
python3.11 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
pip install -e .
```

## API

Defined across these modules (there is no package `__init__.py`):

| Name | From | Description |
| --- | --- | --- |
| `detect_and_crop` | `detector.py` | Runs YOLO inference over a list of images and returns rectified crop entries (see below) |
| `blur_score` | `detector.py` | Laplacian-variance blur score for an image; lower is blurrier |
| `IMG_EXTS` | `detector.py` | Set of recognized image file extensions |
| `rectify_obb_crop` | `geometry.py` | Perspective-warps an oriented quad into an upright crop |
| `normalize_label_direction` | `geometry.py` | Flips a horizontal crop 180° if needed, using a detail-gradient heuristic |
| `axis_aligned_crop` | `geometry.py` | Pads and crops a legacy axis-aligned `xyxy` box |
| `axis_aligned_envelope` | `geometry.py` | Axis-aligned `xyxy` envelope around an oriented quad's 4 corners |
| `order_quad_points` | `geometry.py` | Orders a quad's corners as top-left, top-right, bottom-right, bottom-left |
| `expand_quad` | `geometry.py` | Expands a quad around its center by a padding ratio, clamped to image bounds |

### `detect_and_crop(model, image_paths, conf, pad_ratio, save_crops_dir, blur_thresh=0.0)`

1. **Load & optionally skip blurry frames** — reads each image with
   `cv2.imread`. If `blur_thresh > 0`, computes `blur_score` and skips the
   frame if it's below the threshold, adding it to `skipped` with
   `reason: "blurry"`.
2. **Run YOLO inference** — `model.predict(source=image, conf=conf,
   verbose=False)`. Branches on whether the model produced oriented boxes
   (`result.obb`) or plain axis-aligned boxes (`result.boxes`).
3. **For each detected box:**
   - **OBB case**: pulls the 4 corner points (`xyxyxyxy`), orders them
     consistently (`order_quad_points`), computes a legacy axis-aligned
     envelope (`axis_aligned_envelope`) for backward-compat, then
     perspective-warps the quad into an upright rectangular crop
     (`rectify_obb_crop`) and runs `normalize_label_direction` to flip it
     180° if needed so the dense Data Matrix end lands on the left.
   - **Plain box case**: just pads and crops the axis-aligned `xyxy` box
     (`axis_aligned_crop`); no rotation/orientation logic since there's no
     angle info.
   - Skips the box entirely if the resulting crop is empty (e.g. a
     degenerate/too-small OBB).
4. Optionally saves the crop to `save_crops_dir` as
   `{image_stem}_box{index}.jpg` if requested.
5. Builds a result dict per box — image name, box index, YOLO confidence,
   `xyxy`, ordered `polygon`, whether it was OBB, the grayscale crop (kept
   in-memory under `"gray"`), plus placeholder fields a downstream
   decode/OCR pipeline fills in (`decoded`, `ocr`, `decode_angle`,
   `orientation_confident`, etc.).

Returns `(entries, skipped)`, where `skipped` is a list of
`{"image", "image_name", "reason", ...}` dicts — one per whole image that
was never detected on (see `detect_and_crop`'s docstring for the full
shape).

## HTTP API

`server.py` exposes `detect_and_crop` as a FastAPI microservice ("Image
Crops" in Swagger UI). The model is loaded once at startup (via FastAPI's
`lifespan`) and stays warm across requests, avoiding per-call model-load /
MPS cold-start cost. Detection-only — no barcode decoding or OCR (see
`../read_qr_ocr` for that).

### Running

```bash
MODEL_PATH=weight.pt uvicorn server:app --host 0.0.0.0 --port 8818
```

Swagger UI / interactive docs at `/docs` (OpenAPI schema at `/openapi.json`).

### Environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `MODEL_PATH` | `weight.pt` | Path to the YOLO model weights to load at startup. |
| `DEVICE` | auto | Inference device (`cpu`, `mps`, `cuda:0`, ...). Unset auto-detects MPS on Apple Silicon, else CPU. |
| `SAVE_CROPS_DIR` | unset | Server-side fallback folder for saved crops, used when a request doesn't pass `workspace`. Fixed at startup (not a request param) since a client-supplied write path would be an arbitrary-file-write vector. |

### `GET /health`

Returns `{"status", "device", "save_crops_dir"}` — useful for confirming
which device the model loaded on and whether `SAVE_CROPS_DIR` is set.

### `POST /tasks`

Reads an image from server-local disk (no file upload — `image_path` must be
a path the server process can read) and returns detected boxes with their
rectified crops.

| Query param | Required | Default | Description |
| --- | --- | --- | --- |
| `image_path` | yes | — | Server-local filesystem path to the image to detect+crop. |
| `workspace` | no | — | Root folder to write results into. Crops + `output.json` are written to `{workspace}/image_crops/`. Wins over `SAVE_CROPS_DIR` when both are set; if neither is set, crops aren't saved to disk. |
| `confidence` | no | `0.25` | YOLO detection confidence threshold (0-1). Boxes scoring below this are discarded. |
| `pad_ratio` | no | `0.15` | Fractional margin added around each detected box before cropping (`0.15` = 15% padding). |
| `blur_threshold` | no | `0.0` | Minimum Laplacian-variance sharpness score required to process the image. The whole image is skipped (counted in `n_skipped`) if its score falls below this. `0` (default) disables the check. |

Response shape:

```jsonc
{
  "input": {
    "image_name": "image.jpeg",
    "image_path": "/abs/path/to/image.jpeg",
    "workspace": "/abs/path/to/workspace",  // or null
    "confidence": 0.25,
    "pad_ratio": 0.15,
    "blur_threshold": 0.0
  },
  "output": {
    "boxes": [
      {
        "box_index": 0,
        "yolo_conf": 0.8785,
        "xyxy": [1728.7, 865.5, 2120.4, 987.1],
        "polygon": [[...], [...], [...], [...]],
        "is_obb": true,
        "layout_angle": 0,
        "layout_margin": 0.228,
        "image": "/abs/path/to/workspace/image_crops/image_box0.jpg",  // this box's crop, or null if not saved
        "image_name": "image_box0.jpg"                                  // basename of the above, or null
      }
    ],
    "n_skipped": 0,
    "crops_dir": "/abs/path/to/workspace/image_crops"  // or null
  },
  "start_at": "2026-08-20T09:35:11.714928+00:00",
  "finished_at": "2026-08-20T09:35:12.702391+00:00",
  "success": true
  // "error": "..."  // present only when success is false
}
```

`image`/`image_name` on each box refer to that box's own saved crop file, not
the shared input image (which is reported once, under `input`). If
`workspace`/`SAVE_CROPS_DIR` resolve to a save directory, this same response
body is also written to `{crops_dir}/output.json`.
