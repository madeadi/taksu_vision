# split_pdf

Splits a PDF into single-page PDF files, via [pdfcpu](https://github.com/pdfcpu/pdfcpu).
No image rendering, no OCR — page-splitting only.

## Setup

Requires Go 1.25+.

```bash
go build -o split_pdf .
```

## HTTP API

Plain `net/http` server (this service is Go, not Python/FastAPI like the
other services here) — but the request/response conventions mirror
`../crop_boxes` and `../detect_boxes`: `GET /health`, `POST /tasks` with
server-local paths as query params, and the same JSON envelope shape.

### Running

```bash
WORKSPACE_ROOT=/path/to/shared/storage ./split_pdf --host 0.0.0.0 --port 8823
```

`WORKSPACE_ROOT` is required (same shared-disk path as `../core` and every
other service) — the server refuses to start without it.

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
| `pages_out_dir` | query | yes | — | Directory (relative to the workspace) to write single-page PDFs into. Created (with parents) if it doesn't exist. |
| `json_output_path` | query | no | — | Path (relative to the workspace) to write the response JSON to. If omitted, the response isn't written to disk. |

Output files are named `{pdf_stem}_{page}.pdf` (1-based page number), e.g.
`document.pdf` → `document_1.pdf`, `document_2.pdf`, ...

Example request:

```
POST /tasks?workspace_id=550e8400-e29b-41d4-a716-446655440000&pdf_path=document.pdf&pages_out_dir=pages&json_output_path=pages/output.json
```

Response shape:

```jsonc
{
  "input": {
    "workspace_id": "550e8400-e29b-41d4-a716-446655440000",
    "pdf_path": "document.pdf",
    "pdf_name": "document.pdf",
    "pages_out_dir": "pages",
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

`workspace_id` missing/unknown, `pdf_path`/`pages_out_dir` missing/empty, a
path escaping the workspace, or `pdf_path` not found return `400` before any
splitting is attempted. Failures during splitting itself (e.g. a malformed
PDF) are reported as `success: false` with an `error` message in the
response body (HTTP status is still `200`).
