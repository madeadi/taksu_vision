# read_qr_ocr

Barcode decoding (Data Matrix / Aztec) and OCR fallback for already-cropped,
already-straightened label images, as a standalone package. Takes crop
*files* on disk as input (e.g. the crops written by `../image_crops`'s
`POST /tasks`) — this package does no detection or geometry correction of
its own, just decoding.

Copied out of `../obb_label/label_pipeline_v2` originally, then reworked to
drop its dependency on `image_crops`/detection metadata (`is_obb`,
`layout_margin`, `box_index`, ...) so it stays usable purely from crop files.
Not currently installed as a package anywhere (no `pyproject.toml`) — its
modules (`decoder.py`, `entry.py`, `result.py`) are imported as bare local
modules by `server.py`, which is run with this directory as the working
directory.

## Setup

Requires Python 3.11 or 3.12 (`ocrmac`, `easyocr` may not yet have wheels for
newer versions). `ocrmac` is macOS-only (uses Apple's Vision framework via
PyObjC) — see the caution under `OCR_ENGINE` below before using it as a
server. `easyocr` works cross-platform.

```bash
python3.11 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

## API

| Name | From | Description |
| --- | --- | --- |
| `load_crop_entries` | `decoder.py` | Reads a list of crop image file paths from disk into `CropEntry` dicts (grayscale `gray` array + identity fields) |
| `decode_crops` | `decoder.py` | Attempts a Data Matrix / Aztec decode on each entry's crop across a preprocessing ensemble (raw, CLAHE, upscaled, Otsu/adaptive threshold) |
| `ocr_failed_crops` | `decoder.py` | Runs OCR at all four right-angle rotations on decode failures, scoring each for the most plausible orientation and serial-number match |
| `OCR_ENGINES` | `decoder.py` | `{"easyocr": ..., "ocrmac": ...}` factory map for the two supported OCR engines |
| `CropEntry`, `OcrResult` | `entry.py` | `TypedDict`s describing the per-crop dict shape these functions read/mutate |
| `serializable_results` | `result.py` | Strips in-memory image arrays / temp-file paths from `entries`, leaving JSON-safe fields |

### `load_crop_entries(crop_paths)`

Reads each path with `cv2.imread`, converts to grayscale, and returns a
`list[CropEntry]` with `image`/`image_name`/`_crop_path`/`gray` set and the
decode/OCR output fields initialized to their empty defaults. Skips (with a
warning) any path that fails to read.

### `decode_crops(entries, upscale_factor)`

Mutates `entries` in place (no return value). Expects each entry to already
have a `"gray"` key (see `load_crop_entries`). For each entry:

1. Resets `decode_method`, `decode_attempts`, `decode_failure_reason`.
2. Tries a sequence of increasingly expensive preprocessing variants (raw →
   CLAHE → upscaled nearest/cubic → sharpened → Otsu threshold → adaptive
   threshold, plus inverted versions of the threshold steps), decoding each
   with `zxingcpp.read_barcodes` (Data Matrix / Aztec formats).
3. On first success: stores the decoded text(s) in `entry["decoded"]`,
   records which method worked, and if the barcode reports an orientation,
   rotates the crop (in-memory and on-disk, if `entry["_crop_path"]` is set)
   to be upright.
4. On total failure: sets `entry["decode_failure_reason"]` to `"too_small"`,
   `"blurred"`, or `"decode_exhausted"`.

### `ocr_failed_crops(entries, engine, preprocess, scale)`

Runs the given OCR `engine` (`"easyocr"` or `"ocrmac"`) on every entry whose
`decoded` list is empty, trying all four right-angle rotations (0/90/180/270
— there's no detection metadata here to narrow that down), scoring each
rotation's text for plausibility (favoring a serial-number-shaped match), and
keeping the best-scoring rotation's OCR result in `entry["ocr"]`. Returns the
list of entries it processed.

## HTTP API

`server.py` exposes this as a FastAPI microservice ("Read QR OCR" in Swagger
UI). There's no model to keep warm (barcode decode via `zxingcpp` is
stateless), so unlike `image_crops`/`pipeline_nuh` there's no startup
model-load step.

### Running

```bash
.venv/bin/uvicorn server:app --host 0.0.0.0 --port 8819
```

Swagger UI / interactive docs at `/docs`.

### Environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `SAVE_RESULTS_DIR` | unset | Server-side fallback folder for `output.json`, used when a request doesn't pass `workspace`. |
| `OCR_ENGINE` | `easyocr` | Which `OCR_ENGINES` entry to use when `run_ocr=true`. **Not `ocrmac` by default**: `ocrmac` calls Apple's Vision framework via PyObjC, which has been observed to `SIGBUS`-crash the whole server process (native `EXC_BAD_ACCESS` inside ImageIO's EXIF parsing) on some inputs — a crash Python can't catch. `easyocr` is pure PyTorch, so failures there surface as normal exceptions instead. Set to `ocrmac` only if you understand and accept that risk. |

### `GET /health`

Returns `{"status", "save_results_dir"}`.

### `POST /tasks`

| Query param | Required | Default | Description |
| --- | --- | --- | --- |
| `crops_dir` | yes | — | Server-local directory of already-cropped, already-straightened images to decode. |
| `workspace` | no | — | Root folder to write results into. `output.json` is written to `{workspace}/read_qr_ocr/`. Wins over `SAVE_RESULTS_DIR` when both are set. |
| `upscale` | no | `3.0` | Upscale factor tried by the decode ensemble on difficult symbols. |
| `run_ocr` | no | `false` | Run OCR (via `OCR_ENGINE`) on crops that fail barcode decode. Off by default — slower than barcode decode alone. |
| `ocr_preprocess` | no | `true` | When `run_ocr` is true: upscale + CLAHE-enhance each crop before OCR. |
| `ocr_scale` | no | `3.0` | When `run_ocr` and `ocr_preprocess` are true: the upscale factor applied before OCR. |

Response shape:

```jsonc
{
  "input": {
    "crops_dir": "/abs/path/to/image_crops",
    "workspace": "/abs/path/to/workspace",  // or null
    "upscale": 3.0,
    "run_ocr": false,
    "ocr_preprocess": true,
    "ocr_scale": 3.0
  },
  "output": {
    "boxes": [
      {
        "image": "/abs/path/to/image_crops/image2_box0.jpg",
        "image_name": "image2_box0.jpg",
        "decoded": ["NB26-12345,BR,A24,AAA"],
        "decode_angle": 1,
        "decode_method": "raw",
        "decode_attempts": 1,
        "decode_failure_reason": null,
        "ocr": [],
        "ocr_angle": null,
        "orientation_confident": true,
        "orientation_margin": null,
        "orientation_source": null
      }
    ],
    "n_decoded": 1,
    "n_ocr_read": 0
  },
  "start_at": "2026-08-20T09:35:11.714928+00:00",
  "finished_at": "2026-08-20T09:35:12.702391+00:00",
  "success": true
  // "error": "..."  // present only when success is false
}
```

If `workspace`/`SAVE_RESULTS_DIR` resolve to a save directory, this same
response body is also written to `{save_dir}/output.json`.
