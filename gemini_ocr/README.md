# gemini_ocr

OCR for already-cropped, already-straightened images via the Gemini API, as
a standalone package. Takes crop *files* on disk as input (e.g. the crops
written by `../image_crops`'s `POST /tasks`) — this package does no
detection or geometry correction of its own, just OCR, using Gemini 2.5
Flash-Lite by default.

## Setup

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

Requires a Gemini API key with access to the Gemini API (see
[ai.google.dev](https://ai.google.dev)).

## API

| Name | From | Description |
| --- | --- | --- |
| `ocr_images` | `ocr.py` | OCRs a list of image files with Gemini, in parallel, returning one result dict per image |
| `IMG_EXTS` | `ocr.py` | Set of recognized image file extensions |
| `DEFAULT_PROMPT` | `ocr.py` | Default OCR prompt sent alongside each image |

### `ocr_images(client, image_paths, model, prompt=DEFAULT_PROMPT, max_concurrency=4)`

For each path in `image_paths`, reads the file's bytes and sends them to
Gemini (`client.models.generate_content`) alongside `prompt`, asking for
the text visible in the image. Requests run in parallel via a thread pool
(`max_concurrency` workers) since each call is a network-bound API request.

Returns a list of dicts, one per input path, in the same order as
`image_paths`:

```jsonc
{
  "image": "/abs/path/to/image_crops/image2_box0.jpg",
  "image_name": "image2_box0.jpg",
  "text": "NB26-12345,BR,A24,AAA",
  "error": null
}
```

A per-image failure (unreadable file, API error, ...) never raises — it's
captured in that image's `error` field (with `text: null`) so one bad image
doesn't abort the rest of the batch.

## HTTP API

`server.py` exposes this as a FastAPI microservice ("Gemini OCR" in Swagger
UI). There's no model to keep warm (the Gemini client just wraps HTTP calls
to Google's API), so unlike `image_crops`/`pipeline_nuh` there's no startup
model-load step.

### Running

```bash
GEMINI_API_KEY=... .venv/bin/uvicorn server:app --host 0.0.0.0 --port 8820
```

Swagger UI / interactive docs at `/docs`.

### Environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `GEMINI_API_KEY` | — | API key for the Gemini API. Required (falls back to `GOOGLE_API_KEY` if set instead); the server refuses to start without one. |
| `GEMINI_MODEL` | `gemini-2.5-flash-lite` | Default Gemini model used for OCR. Overridable per-request via the `model` query param. |
| `MAX_CONCURRENCY` | `4` | Default max number of Gemini requests run in parallel per `/tasks` call. Overridable per-request via the `max_concurrency` query param. |
| `SAVE_RESULTS_DIR` | unset | Server-side fallback folder for `output.json`, used when a request doesn't pass `workspace`. |

### `GET /health`

Returns `{"status", "model", "save_results_dir"}`.

### `POST /tasks`

| Query param | Required | Default | Description |
| --- | --- | --- | --- |
| `images_dir` | yes | — | Server-local directory of already-cropped images to OCR. |
| `workspace` | no | — | Root folder to write results into. `output.json` is written to `{workspace}/gemini_ocr/`. Wins over `SAVE_RESULTS_DIR` when both are set. |
| `model` | no | `GEMINI_MODEL` | Gemini model to OCR with (e.g. `gemini-2.5-flash-lite`, `gemini-2.5-flash`, `gemini-2.5-pro`). |
| `prompt` | no | `DEFAULT_PROMPT` | Prompt sent alongside each image. |
| `max_concurrency` | no | `MAX_CONCURRENCY` | Max number of Gemini requests to run in parallel. |

Response shape:

```jsonc
{
  "input": {
    "images_dir": "/abs/path/to/image_crops",
    "workspace": "/abs/path/to/workspace",  // or null
    "model": "gemini-2.5-flash-lite",
    "prompt": "Read all text visible in this image. ...",
    "max_concurrency": 4
  },
  "output": {
    "results": [
      {
        "image": "/abs/path/to/image_crops/image2_box0.jpg",
        "image_name": "image2_box0.jpg",
        "text": "NB26-12345,BR,A24,AAA",
        "error": null
      }
    ],
    "n_processed": 1,
    "n_failed": 0
  },
  "start_at": "2026-08-20T09:35:11.714928+00:00",
  "finished_at": "2026-08-20T09:35:12.702391+00:00",
  "success": true
  // "error": "..."  // present only when the whole batch call itself failed
}
```

If `workspace`/`SAVE_RESULTS_DIR` resolve to a save directory, this same
response body is also written to `{save_dir}/output.json`.
