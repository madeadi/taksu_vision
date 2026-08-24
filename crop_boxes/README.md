# crop_boxes

Crop extraction from pre-detected boxes, as a standalone package. Given an
image and a list of boxes (axis-aligned `xyxy` or oriented `polygon`
corners), it produces upright, padded crops ready for downstream barcode
decoding or OCR. No detection — see `../detect_boxes` for that, and no
shared code with it (this package has its own copy of anything it needs).

## Setup

Requires Python 3.11 or 3.12.

```bash
python3.11 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

## API

Defined in `cropper.py` (there is no package `__init__.py`):

| Name | Description |
| --- | --- |
| `crop_boxes` | Crops a list of boxes out of one image and (optionally) saves the crop files |
| `rectify_obb_crop` | Perspective-warps an oriented quad into an upright crop |
| `normalize_label_direction` | Flips a horizontal crop 180° if needed, using a detail-gradient heuristic |
| `axis_aligned_crop` | Pads and crops an axis-aligned `xyxy` box |
| `order_quad_points` | Orders a quad's corners as top-left, top-right, bottom-right, bottom-left |
| `expand_quad` | Expands a quad around its center by a padding ratio, clamped to image bounds |

### `crop_boxes(image_path, boxes, pad_ratio, save_crops_dir, timing=None)`

Each item in `boxes` is `{"box_index", "is_obb", "xyxy", "polygon"}` — the
same shape `../detect_boxes`'s `POST /tasks` returns per box, so its
`output.boxes` can be passed straight through.

1. **OBB case** (`is_obb: true`): perspective-warps the quad's 4 corner
   points (`polygon`) into an upright rectangular crop (`rectify_obb_crop`),
   then runs `normalize_label_direction` to flip it 180° if needed so the
   dense Data Matrix end lands on the left.
2. **Plain case** (`is_obb: false`): pads and crops the axis-aligned `xyxy`
   box (`axis_aligned_crop`); no rotation/orientation logic since there's no
   angle info.
3. Skips the box entirely if the resulting crop is empty (e.g. a
   degenerate/too-small OBB).
4. Optionally saves the crop to `save_crops_dir` as
   `{image_stem}_box{index}.jpg` if requested.

Returns a list of per-box dicts: `{"box_index", "is_obb", "crop_path",
"layout_angle", "layout_margin"}`. `crop_path` is `None` if `save_crops_dir`
is falsy.

## HTTP API

`server.py` exposes `crop_boxes` as a FastAPI microservice ("Crop Boxes" in
Swagger UI). Stateless — no model to keep warm, so there's no startup cost.
Crop-only — no detection, no barcode decoding, no OCR.

### Running

```bash
WORKSPACE_ROOT=/path/to/shared/storage uvicorn server:app --host 0.0.0.0 --port 8820
```

`WORKSPACE_ROOT` is required (same shared-disk path as `../core` and every
other service) — the server refuses to start without it.

Swagger UI / interactive docs at `/docs` (OpenAPI schema at `/openapi.json`).

### `GET /health`

Returns `{"status": "ok"}`.

### `POST /tasks`

Takes `workspace_id`, `image_path`, `pad_ratio`, `crops_out_dir`, and
`json_output_path` as query params, and `boxes` as a raw JSON array request
body (too complex a structure for a query param). Every path param is
relative to the workspace, resolved against
`$WORKSPACE_ROOT/{workspace_id}/files/`. Workspaces themselves are created
(and files uploaded into them) via `../core` (see `../core/README.md`).

| Param | Where | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `workspace_id` | query | yes | — | Workspace to read/write files in. |
| `image_path` | query | yes | — | Path (relative to the workspace) of the image to crop from. |
| `pad_ratio` | query | no | `0.15` | Fractional margin added around each box before cropping (`0.15` = 15% padding). |
| `crops_out_dir` | query | yes | — | Directory (relative to the workspace) to write crop files into. Created (with parents) if it doesn't exist. |
| `json_output_path` | query | no | — | Path (relative to the workspace) to write the response JSON to. If omitted, the response isn't written to disk — only returned in the HTTP response. |
| `boxes` | body | yes | — | JSON array of boxes to crop: `{"box_index": int, "is_obb": bool, "xyxy": [x1,y1,x2,y2] or null, "polygon": [[x,y],...] or null}`. `xyxy` is required when `is_obb` is `false`; `polygon` (4 `[x, y]` corners, any order) is required when `is_obb` is `true`. |

Example request:

```
POST /tasks?workspace_id=550e8400-e29b-41d4-a716-446655440000&image_path=image.jpeg&crops_out_dir=crops&json_output_path=crops/output.json&pad_ratio=0.15
Content-Type: application/json

[
  {
    "box_index": 0,
    "is_obb": true,
    "xyxy": [1728.7, 865.5, 2120.4, 987.1],
    "polygon": [[1735.0, 870.0], [2115.0, 865.5], [2120.4, 982.0], [1728.7, 987.1]]
  }
]
```

Response shape:

```jsonc
{
  "input": {
    "image_name": "image.jpeg",
    "image_path": "image.jpeg",
    "pad_ratio": 0.15,
    "crops_out_dir": "crops",
    "json_output_path": "crops/output.json",  // or null
    "box_count": 1
  },
  "output": {
    "crops": [
      {
        "box_index": 0,
        "is_obb": true,
        "crop_path": "crops/image_box0.jpg",
        "layout_angle": 0,
        "layout_margin": 0.228
      }
    ],
    "crops_dir": "crops"
  },
  "start_at": "2026-08-21T09:35:11.714928+00:00",
  "finished_at": "2026-08-21T09:35:11.802391+00:00",
  "success": true
  // "error": "..."  // present only when success is false
}
```

This same response body is also written to `json_output_path` when it's
provided; otherwise it's only returned in the HTTP response, not saved to
disk.
