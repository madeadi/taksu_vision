# split_pdf

Splits a PDF into single-page files, via [pdfcpu](https://github.com/pdfcpu/pdfcpu).
Default output is single-page PDFs (no rendering). `output_format=jpg` renders
each page to a JPEG instead, via Ghostscript — for feeding pages straight into
an image-only consumer like `../detect_boxes`. No OCR either way.

## Setup

Requires Go 1.25+. `output_format=jpg` additionally requires Ghostscript
(`gs`) on `PATH` — not needed for the default PDF-splitting mode.

```bash
go build -o split_pdf .
```

## HTTP API

Request/response conventions mirror `../crop_boxes` and `../detect_boxes`:
`GET /health`, `POST /tasks` with server-local paths as query params, and
the same JSON envelope shape. Built on [huma](https://huma.rocks) over the
stdlib `net/http` mux, like `../core`, so the API is self-documenting —
interactive docs at `GET /docs` (Swagger UI) and the raw spec at
`GET /openapi.json`, both generated from the Go input/output structs in
`main.go` rather than hand-maintained.

### Running

```bash
WORKSPACE_ROOT=/path/to/shared/storage ./split_pdf --host 0.0.0.0 --port 8823
```

`WORKSPACE_ROOT` is required (same shared-disk path as `../core` and every
other service) — the server refuses to start without it.

Swagger UI / interactive docs at `/docs` (OpenAPI schema at
`/openapi.json`).

### `GET /health`

Returns `{"status": "ok"}`.

### `POST /tasks`

Every path param below is relative to the given `workspace_id`, resolved
against `$WORKSPACE_ROOT/{workspace_id}/files/` — no absolute paths.
Workspaces themselves are created (and files uploaded into them) via
`../core` (see `../core/README.md`).

| Param | Where | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `workspace_id` | query | yes | — | Workspace to read/write files in. |
| `pdf_path` | query | yes | — | Path (relative to the workspace) of the PDF to split. |
| `pages_out_dir` | query | yes | — | Directory (relative to the workspace) to write single-page files into. Created (with parents) if it doesn't exist. |
| `output_format` | query | no | `pdf` | `pdf` splits into single-page PDFs (default). `jpg` renders each page to a JPEG instead (via Ghostscript). |
| `dpi` | query | no | `200` | Render resolution in DPI. Only used when `output_format=jpg`. |
| `json_output_path` | query | no | — | Path (relative to the workspace) to write the response JSON to. If omitted, the response isn't written to disk. |

Output files are named `{pdf_stem}_{page}.pdf` (or `.jpg` in jpg mode),
1-based page number, e.g. `document.pdf` → `document_1.pdf`, `document_2.pdf`, ...

Example request (JPEG pages at 150 DPI):

```
POST /tasks?workspace_id=550e8400-e29b-41d4-a716-446655440000&pdf_path=document.pdf&pages_out_dir=pages&output_format=jpg&dpi=150&json_output_path=pages/output.json
```

Response shape (huma adds the `$schema` field to every JSON response,
pointing at that response's generated JSON Schema; the file written to
`json_output_path`, below, does not include it):

```jsonc
{
  "$schema": "http://localhost:8823/schemas/TaskResult.json",
  "input": {
    "workspace_id": "550e8400-e29b-41d4-a716-446655440000",
    "pdf_path": "document.pdf",
    "pdf_name": "document.pdf",
    "pages_out_dir": "pages",
    "output_format": "pdf",
    "dpi": 200,  // echoed even in pdf mode (its default), though unused there
    "json_output_path": "pages/output.json"  // omitted if not provided
  },
  "output": {
    "pages": [
      { "page_number": 1, "page_path": "pages/document_1.pdf" },
      { "page_number": 2, "page_path": "pages/document_2.pdf" }
    ],
    "pages_dir": "pages",
    "page_count": 2
  },
  "start_at": "2026-08-24T06:08:14.000000Z",
  "finished_at": "2026-08-24T06:08:14.100000Z",
  "success": true
  // "error": "..."  // present only when success is false
}
```

This same response body is also written to `json_output_path` when it's
provided; otherwise it's only returned in the HTTP response, not saved to
disk. `page_path` (and every other path field) is workspace-relative, so it
can be fed straight into the next service's `/tasks` call.

`workspace_id`, `pdf_path`, or `pages_out_dir` missing or empty, or an
`output_format` other than `pdf`/`jpg`, return `422` (huma's standard
request-validation response) before any splitting is attempted. A
`workspace_id` that doesn't exist, a path escaping the workspace, or a
`pdf_path` not found within it return `400`. Failures during splitting or
rendering itself (e.g. a malformed PDF, or `gs` missing from `PATH` in jpg
mode) are reported as `success: false` with an `error` message in the
response body (HTTP status is still `200`).
