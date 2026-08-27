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
| `defaultInstruction` | `ocr.go` | Default extraction instruction used when a request omits `instruction` |
| `buildPrompt` | `prompt.go` | Combines `instruction`/`formatted_output`/`patterns`/`remove_patterns` into one prompt sent alongside each image |

### `ocrImages(ctx, client, imagePaths []string, model, prompt string, maxConcurrency int) []OCRResult`

For each path in `imagePaths`, reads the file's bytes and sends them to
Gemini (`client.Models.GenerateContent`) alongside `prompt` — the combined
extraction prompt built by `buildPrompt` — asking it to reply with a JSON
object describing what it read. Requests run in parallel, bounded by
`maxConcurrency` goroutines, since each call is a network-bound API request.

Returns one `OCRResult` per input path, in the same order as `imagePaths`:

```jsonc
{
  "image": "/abs/path/to/crops/image2_box0.jpg",
  "image_name": "image2_box0.jpg",
  "detected_text": "NB26-12345,BR,A24,AAA",
  "matched_patterns": ["6-digit plate number"],
  "formatted_output": {"plate_number": "NB26-12345", "region_code": "BR"},
  "input_tokens": 263,
  "output_tokens": 41,
  "cost_usd": 0.0000428,
  "error": null
}
```

`detected_text` is the specific answer to `instruction` — e.g. for
"What's the invoice number?" it's just the invoice number, not a transcript
of the whole document. (The exception: `defaultInstruction`, used when a
request omits `instruction`, asks for a full read, so its "answer" is
everything visible.) `formatted_output` stays `{}` unless the request
provided a `formatted_output` example to populate.

A per-image failure never aborts the batch — it's captured in that image's
`error` field instead. An unreadable file or a failed API call leaves every
other field `null`. A malformed (non-JSON) model reply leaves just
`detected_text`, `matched_patterns`, and `formatted_output` `null`/empty —
`input_tokens`/`output_tokens`/`cost_usd` are still populated, since the API
call itself succeeded and consumed billable tokens.

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

`WORKSPACE_ROOT` (or `config.yaml`'s `workspace_root`, see below) is required
(same shared-disk path as `../core` and every other service) — the server
refuses to start without it.

Swagger UI / interactive docs at `/docs`.

### Configuration

Everything except `--host`/`--port` can be set via a YAML config file
instead of env vars. Copy `config.example.yaml` to `config.yaml` (gitignored
— it's deployment-specific, and may hold a secret, so never commit a
filled-in copy) and fill in what you need, or point elsewhere with
`--config path/to/file.yaml`:

```yaml
gemini_api_key: ""
gemini_model: gemini-2.5-flash-lite
max_concurrency: 4
workspace_root: /path/to/shared/storage
save_results_dir: gemini_ocr/output.json
```

The config file is optional, and so is every field in it — anything left
unset falls back to its env var below, then to the hardcoded default (or, for
`gemini_api_key`/`workspace_root`, a fatal startup error if neither is set).
There's no need to touch `scripts/run_pipeline.sh` or other existing
env-var-only setups; they keep working unchanged. Loaded via
[viper](https://github.com/spf13/viper).

Editing `config.yaml` doesn't take effect until either the server restarts,
or you call:

```
POST /config/refresh
```

which re-reads the file at the same `--config` path and re-resolves every
setting (including recreating the Gemini client if `gemini_api_key`
changed). Returns the newly active settings:

```jsonc
{
  "success": true,
  "model": "gemini-2.5-flash-lite",
  "max_concurrency": 4,
  "workspace_root": "/path/to/shared/storage",
  "save_results_dir": "gemini_ocr/output.json"
}
```

In-flight `/tasks` requests keep using whatever settings were active when
they started; only requests made after the refresh see the new values.

### Environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `GEMINI_API_KEY` | — | API key for the Gemini API. Checked after `config.yaml`'s `gemini_api_key`, before falling back to `GOOGLE_API_KEY`; the server refuses to start if none of the three is set. |
| `GEMINI_MODEL` | `gemini-2.5-flash-lite` | Default Gemini model used for OCR. Overridable per-request via the `model` query param, or via `config.yaml`'s `gemini_model`. |
| `MAX_CONCURRENCY` | `4` | Default max number of Gemini requests run in parallel per `/tasks` call. Overridable per-request via the `max_concurrency` query param, or via `config.yaml`'s `max_concurrency`. |
| `WORKSPACE_ROOT` | — | Shared-disk directory workspaces live under. Required (here or via `config.yaml`'s `workspace_root`) — the server refuses to start without it. |
| `SAVE_RESULTS_DIR` | `gemini_ocr/output.json` | Default workspace-relative path for `output.json`, used when a request doesn't pass `json_output_path`. Also settable via `config.yaml`'s `save_results_dir`. |
| `CORS_ALLOWED_ORIGINS` | `*` (any origin) | Comma-separated list of origins allowed to call this API cross-origin (e.g. the web app's dev server origin). Same convention as `../core` and `../detect_boxes`. |

### `GET /health`

Returns `{"status", "model", "save_results_dir"}`.

### `GET /logs`

Returns `{"lines": [...]}` — the most recent (up to 1000) lines this
process has logged, oldest first, kept in memory only (not persisted across
restarts; `scripts/run_pipeline.sh` separately redirects stdout/stderr to a
log file on disk). Backs the web app's Logs tab (see below).

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
| `max_concurrency` | no | `MAX_CONCURRENCY` | Max number of Gemini requests to run in parallel. |

The request body is a JSON object, all fields optional:

| Body field | Description |
| --- | --- |
| `filenames` | File names within `images_dir` to restrict OCR to. If omitted, every image file in `images_dir` is processed. |
| `instruction` | Free-text instruction describing what to extract from each image. If omitted, falls back to a built-in general-OCR instruction. |
| `formatted_output` | Example JSON object showing the shape/keys/sample values to populate — an example, not a formal schema. If omitted, each result's `formatted_output` is `{}`. |
| `patterns` | Text pattern descriptions (e.g. `"16-digit NIK number"`) to check the detected text against. Matches are echoed back verbatim in each result's `matched_patterns`. If omitted, `matched_patterns` is `[]`. |
| `remove_patterns` | Text patterns to strip out of the detected text *before* checking it against `patterns`. If omitted, no filtering is applied. |

These four fields are combined server-side into one extraction prompt,
applied identically to every image in the batch, followed by a fixed
(non-configurable) instruction telling the model to reply with only the JSON
object shown below — using blank/empty values for anything it can't
extract.

Response shape:

```jsonc
{
  "input": {
    "images_dir": "id_cards",
    "filenames": null,
    "json_output_path": null,
    "model": "gemini-2.5-flash-lite",
    "instruction": "Read the ID card and extract the fields below.",
    "formatted_output": {"name": "string", "nik": "string"},
    "patterns": ["16-digit NIK number"],
    "remove_patterns": ["WATERMARK"],
    "max_concurrency": 4
  },
  "output": {
    "results": [
      {
        "image": "id_cards/card1.jpg",
        "image_name": "card1.jpg",
        "detected_text": "JOHN DOE 1234567890123456",
        "matched_patterns": ["16-digit NIK number"],
        "formatted_output": {"name": "JOHN DOE", "nik": "1234567890123456"},
        "input_tokens": 340,
        "output_tokens": 52,
        "cost_usd": 0.0000542,
        "error": null
      }
    ],
    "n_processed": 1,
    "n_failed": 0,
    "total_input_tokens": 340,
    "total_output_tokens": 52,
    "total_cost_usd": 0.0000542
  },
  "start_at": "2026-08-27T09:35:11.714928Z",
  "finished_at": "2026-08-27T09:35:12.702391Z",
  "success": true
}
```

This same response body is also written to `json_output_path` (or
`SAVE_RESULTS_DIR` if `json_output_path` is omitted), workspace-relative
like every other path field.

## Web app

`web/` is this service's own web app (React/TypeScript/Vite/Tailwind/
shadcn, same stack as `../core_ui` and `../detect_boxes/web`), per root
`AGENT.md`: a "Try" page (run extraction against images in a workspace), a
"Config" page (view the effective resolved config and trigger
`POST /config/refresh`), and a "Logs" page (`GET /logs`, auto-refreshing).
It talks directly to this service's own HTTP API above and to `../core`'s
workspace API — it does not go through `core_ui`.

### Dev mode: separate Vite server (hot reload)

```bash
cd web
npm install
npm run dev  # http://localhost:8826
```

Configure `VITE_CORE_API_URL` (default `http://localhost:8824`) and
`VITE_GEMINI_OCR_API_URL` (default `http://localhost:8820`) via `.env` (see
`.env.example`). `core`'s and this service's `CORS_ALLOWED_ORIGINS` both
default to `*` (any origin), so no extra CORS setup is needed for local dev.

Register it with `core` with `web_url` pointing at `:8826` (`POST
/services`, overriding `scripts/seed_services.sh`'s default with
`GEMINI_OCR_WEB_URL=http://127.0.0.1:8826` — its default registers the
built-mode `/web` URL below instead) so `core_ui`'s Services page can iframe
it — see `../core/README.md`.

### Built mode: served by this service's own process

`main.go` also serves the built web app itself, so the whole service (API +
web app) is a single process on a single port — no separate frontend
process needed:

```bash
cd web && npm install && npm run build   # writes web/dist
cd ..
GEMINI_API_KEY=... WORKSPACE_ROOT=/path/to/shared/storage \
  ./gemini_ocr --host 0.0.0.0 --port 8820
```

`main.go` mounts `web/dist` at `/web` (after all API routes, so `/health`,
`/tasks`, `/config/refresh`, `/logs`, `/docs`, etc. still take priority)
only if that directory exists — absent a build (or a different location,
overridable with `--web-dir`), this is a no-op and the API behaves as
before. `vite.config.ts` sets `base: "/web/"` for `npm run build` so the
built `index.html`'s asset/favicon URLs match that mount point (the dev
server keeps `base: "/"`, since it's served at its own origin's root
instead). In this mode, register `web_url` as `http://127.0.0.1:8820/web` —
`scripts/seed_services.sh`'s default for `gemini_ocr` — since the web app
lives at that sub-path of the API's own port. Note this only resolves once
`web/dist` has actually been built; run `npm run build` (above) before
starting the pipeline, or the registered URL will 404 until you do. Rebuild
(`npm run build`) and restart the server to pick up frontend changes —
there's no hot reload in this mode.

The built web app's own API calls are same-origin (it's served from the
same process/port), but it still calls `../core`'s workspace API
cross-origin — covered by `core`'s wide-open `CORS_ALLOWED_ORIGINS` default,
so no extra CORS setup is needed for local dev in this mode either.
