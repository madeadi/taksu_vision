# gemini_ocr

OCR for already-cropped, already-straightened images via the Gemini API, as
a standalone Go package. Takes crop *files* on disk as input (e.g. the crops
written by `../crop_boxes`'s `POST /tasks`) — this package does no detection
or geometry correction of its own, just OCR, using Gemini 2.5 Flash-Lite by
default. Built on Google's official [`google.golang.org/genai`](https://pkg.go.dev/google.golang.org/genai)
SDK — a thin HTTP client around the Gemini API, no local model.

## Setup

Requires Go 1.25+.

```bash
go build -o gemini_ocr .
```

Requires a Gemini API key with access to the Gemini API (see
[ai.google.dev](https://ai.google.dev)).

## API

| Name | From | Description |
| --- | --- | --- |
| `ocrImages` | `ocr.go` | OCRs a list of image files with Gemini, in parallel, returning one result per image |
| `imgExts` | `ocr.go` | Set of recognized image file extensions |
| `defaultPrompt` | `ocr.go` | Default OCR prompt sent alongside each image |

### `ocrImages(ctx, client, imagePaths []string, model, prompt string, maxConcurrency int) []OCRResult`

For each path in `imagePaths`, reads the file's bytes and sends them to
Gemini (`client.Models.GenerateContent`) alongside `prompt`, asking for the
text visible in the image. Requests run in parallel, bounded by
`maxConcurrency` goroutines, since each call is a network-bound API request.

Returns one `OCRResult` per input path, in the same order as `imagePaths`:

```jsonc
{
  "image": "/abs/path/to/crops/image2_box0.jpg",
  "image_name": "image2_box0.jpg",
  "text": "NB26-12345,BR,A24,AAA",
  "input_tokens": 263,
  "output_tokens": 13,
  "cost_usd": 0.0000318,
  "error": null
}
```

A per-image failure (unreadable file, API error, ...) never aborts the
batch — it's captured in that image's `error` field (with `text`,
`input_tokens`, `output_tokens`, `cost_usd` all `null`).

`cost_usd` is computed from `ocr.go`'s `modelPricing`, a hardcoded
USD-per-1M-token table for `gemini-2.5-flash-lite`/`-flash`/`-pro` (standard,
non-batch pricing — see [ai.google.dev/gemini-api/docs/pricing](https://ai.google.dev/gemini-api/docs/pricing)).
It's `null` if `model` isn't in that table.

## HTTP API

`main.go` exposes this as an HTTP microservice ("Gemini OCR" in Swagger UI),
built on [huma](https://huma.rocks) like `../core` and `../split_pdf`, so the
API is self-documenting — interactive docs at `GET /docs` and the raw spec
at `GET /openapi.json`. There's no model to keep warm (the Gemini client
just wraps HTTP calls to Google's API), so there's no startup model-load
step.

### Running

```bash
GEMINI_API_KEY=... WORKSPACE_ROOT=/path/to/shared/storage ./gemini_ocr --host 0.0.0.0 --port 8820
```

`WORKSPACE_ROOT` is required (same shared-disk path as `../core` and every
other service) — the server refuses to start without it.

Swagger UI / interactive docs at `/docs`.

### Environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `GEMINI_API_KEY` | — | API key for the Gemini API. Required (falls back to `GOOGLE_API_KEY` if set instead); the server refuses to start without one. |
| `GEMINI_MODEL` | `gemini-2.5-flash-lite` | Default Gemini model used for OCR. Overridable per-request via the `model` query param. |
| `MAX_CONCURRENCY` | `4` | Default max number of Gemini requests run in parallel per `/tasks` call. Overridable per-request via the `max_concurrency` query param. |
| `WORKSPACE_ROOT` | — | Shared-disk directory workspaces live under. Required — the server refuses to start without it. |
| `SAVE_RESULTS_DIR` | `gemini_ocr/output.json` | Default workspace-relative path for `output.json`, used when a request doesn't pass `json_output_path`. |

### `GET /health`

Returns `{"status", "model", "save_results_dir"}`.

### `POST /tasks`

`images_dir` and `json_output_path` are relative to the workspace, resolved
against `$WORKSPACE_ROOT/{workspace_id}/files/`. Workspaces themselves are
created (and files uploaded into them) via `../core` (see
`../core/README.md`).

| Query param | Required | Default | Description |
| --- | --- | --- | --- |
| `workspace_id` | yes | — | Workspace to read/write files in. |
| `images_dir` | yes | — | Directory (relative to the workspace) of already-cropped images to OCR. |
| `json_output_path` | no | — | Path (relative to the workspace) to write the response JSON to. If omitted, falls back to `SAVE_RESULTS_DIR`. |
| `model` | no | `GEMINI_MODEL` | Gemini model to OCR with (e.g. `gemini-2.5-flash-lite`, `gemini-2.5-flash`, `gemini-2.5-pro`). |
| `prompt` | no | built-in default prompt | Prompt sent alongside each image. |
| `max_concurrency` | no | `MAX_CONCURRENCY` | Max number of Gemini requests to run in parallel. |

The request body is an optional JSON object `{"filenames": [...]}` (file
names within `images_dir` to restrict OCR to). If omitted, every image file
in `images_dir` is processed.

Response shape:

```jsonc
{
  "input": {
    "images_dir": "image_crops",
    "filenames": null,
    "json_output_path": null,
    "model": "gemini-2.5-flash-lite",
    "prompt": "Read all text visible in this image. ...",
    "max_concurrency": 4
  },
  "output": {
    "results": [
      {
        "image": "image_crops/image2_box0.jpg",
        "image_name": "image2_box0.jpg",
        "text": "NB26-12345,BR,A24,AAA",
        "input_tokens": 263,
        "output_tokens": 13,
        "cost_usd": 0.0000318,
        "error": null
      }
    ],
    "n_processed": 1,
    "n_failed": 0,
    "total_input_tokens": 263,
    "total_output_tokens": 13,
    "total_cost_usd": 0.0000318
  },
  "start_at": "2026-08-20T09:35:11.714928Z",
  "finished_at": "2026-08-20T09:35:12.702391Z",
  "success": true
}
```

This same response body is also written to `json_output_path` (or
`SAVE_RESULTS_DIR` if `json_output_path` is omitted), workspace-relative
like every other path field.
