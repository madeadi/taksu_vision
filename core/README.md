# Taksu Vision Core

Workspace lifecycle service. Owns creating, uploading into, downloading
from, listing, and deleting workspaces on shared disk at `WORKSPACE_ROOT`.
Every other service in this repo (`split_pdf`, `detect_boxes`, `crop_boxes`,
`read_qr_ocr`, `gemini_ocr`) reads/writes workspace files directly against
that same shared `WORKSPACE_ROOT` rather than calling this service's HTTP
API for ordinary file I/O — `core` is only the lifecycle owner.

Unlike the other services' single `POST /tasks` endpoint, `core` exposes a
small CRUD REST API (create/upload/download/list/delete workspaces) since
its job is stateful lifecycle management, not a one-shot compute task. It
still implements `GET /health` like every other service.

Built on [huma](https://huma.rocks) over the stdlib `net/http` mux, so the
API is self-documenting — interactive docs at `GET /docs` (Swagger UI) and
the raw spec at `GET /openapi.json`, both generated from the Go
input/output structs in `main.go` rather than hand-maintained. None of the
other (Python/FastAPI) services need this since FastAPI already generates
their `/docs`/`/openapi.json` for free.

Workspace *metadata* (`workspace_id`/`created_at`/`expires_at`) is stored in
an embedded [PocketBase](https://pocketbase.io) instance (SQLite, with Go
migrations under `migrations/`) — used here purely as a Go framework/data
layer, not as a separate service. This is what backs the `GET /workspaces`
list endpoint. Workspace *files* are unaffected: they still live on shared
disk at `$WORKSPACE_ROOT/{workspace_id}/files/`, read/written directly by
every other service exactly as before. PocketBase's own admin UI (SQLite
browser, etc.) is available at `/_/` for ops/debugging, and its
auto-generated REST API at `/api/collections/...` — neither is part of the
documented API surface the [`core_ui`](../core_ui) frontend or other
services should rely on; use the huma-documented endpoints below instead.

To log into `/_/`, create a superuser account first:

```bash
go run . superuser upsert you@example.com yourpassword
```

(The one-time installer link printed on first boot is tied to that specific
server process — restarting `core` before opening it, or reopening an old
link from a previous run's logs, fails with "Only superusers can perform
this action". `superuser upsert` always works, regardless of restarts.)

## Setup

Requires Go 1.25+.

```bash
go build -o core .
```

## Running

```bash
WORKSPACE_ROOT=/path/to/shared/storage ./core --host 0.0.0.0 --port 8824
```

Swagger UI / interactive docs at `/docs` (OpenAPI schema at `/openapi.json`).

### Environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `WORKSPACE_ROOT` | — | Shared-disk directory workspaces live under. Required — the server refuses to start without it. Every other service must be started with the same value. |
| `TTL_HOURS` | `168` (7 days) | How long a workspace lives after creation before the sweep removes it. |
| `SWEEP_INTERVAL_MINUTES` | `60` | How often the background sweep checks for expired workspaces. |
| `DB_DIR` | `./pb_data` | Directory for the embedded PocketBase/SQLite metadata store. Deliberately separate from `WORKSPACE_ROOT` — it must not live under it. |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:5173` | Comma-separated list of origins allowed to call this API cross-origin (e.g. the `core_ui` dev/prod origin). |

## On-disk layout

```
$WORKSPACE_ROOT/
  {workspace_id}/
    files/                # everything every service's relative-path params resolve against
```

`{workspace_id}` is a UUIDv4. Other services resolve their path params as
`$WORKSPACE_ROOT/{workspace_id}/files/{relative_path}`, rejecting absolute
paths and anything that would escape `files/` (directly or via a symlink).
Metadata (`created_at`/`expires_at`) for each `{workspace_id}` lives in the
`workspaces` table under `$DB_DIR`, not on disk under the workspace itself.

## TTL sweep

A background goroutine ticks every `SWEEP_INTERVAL_MINUTES` and removes any
workspace whose `expires_at` (set at creation to `created_at + TTL_HOURS`)
has passed. `DELETE /workspaces/{id}` removes a workspace immediately,
independent of the sweep.

## API

### `GET /health`

Returns `{"status", "workspace_root", "workspace_count"}`.

### `POST /workspaces`

Creates a new workspace. No request body.

```jsonc
// 201
{
  "workspace_id": "550e8400-e29b-41d4-a716-446655440000",
  "created_at": "2026-08-24T08:00:00Z",
  "expires_at": "2026-08-31T08:00:00Z"
}
```

### `POST /workspaces/{id}/files?dir=optional/subdir`

Uploads one or more files into the workspace, as `multipart/form-data` with
a repeatable `files` field. `dir` (optional query param) is a
workspace-relative subdirectory to upload into; omit it to upload to the
workspace root. Each uploaded file's name is taken from its multipart
filename (any directory components in that filename are stripped, so an
upload can never land outside `dir`).

```
curl -F "files=@document.pdf" "http://localhost:8824/workspaces/{id}/files"
```

```jsonc
// 200
{
  "workspace_id": "550e8400-...",
  "dir": "",
  "files": [{"name": "document.pdf", "path": "document.pdf", "size": 5440975}]
}
```

`404` if the workspace doesn't exist.

### `GET /workspaces/{id}/files?path=relative/path`

Downloads a single file at the given workspace-relative path (`path` is a
required query param, not a URL path segment — huma's OpenAPI generation
doesn't support multi-segment wildcard path params, and this stays
consistent with how every other service in this repo takes paths). Streams
the file with `Content-Disposition: attachment`. `404` if the workspace or
the path doesn't exist within it.

### `GET /workspaces/{id}/archive?dir=optional/subdir`

Downloads the whole workspace (or just `dir`, if given) as a `.zip`,
streamed directly — no temp file. `404` if the workspace or `dir` doesn't
exist.

### `GET /workspaces`

Lists every workspace's metadata (no file listing — use `GET /workspaces/{id}`
for that).

```jsonc
// 200
{
  "workspaces": [
    {"workspace_id": "550e8400-...", "created_at": "2026-08-24T08:00:00Z", "expires_at": "2026-08-31T08:00:00Z"}
  ]
}
```

### `GET /workspaces/{id}`

Returns workspace metadata plus a recursive file listing.

```jsonc
{
  "workspace_id": "550e8400-...",
  "created_at": "2026-08-24T08:00:00Z",
  "expires_at": "2026-08-31T08:00:00Z",
  "files": [{"path": "document.pdf", "size": 5440975}, {"path": "pages/document_1.pdf", "size": 48428}]
}
```

`404` if the workspace doesn't exist.

### `DELETE /workspaces/{id}`

Deletes a workspace and everything in it, immediately.

```jsonc
{"workspace_id": "550e8400-...", "deleted": true}
```

`404` if the workspace doesn't exist.

## Planned (not yet implemented)

- API Gateway: consolidate all API requests, authentication.
