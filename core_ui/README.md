# Taksu Vision Core UI

Workspace management UI for [`core`](../core) — create/list/delete
workspaces, upload files into one, browse and download its files
individually or as a zip archive. React + Vite + TypeScript + Tailwind CSS +
shadcn/ui.

Talks only to `core`'s documented REST API (`/health`, `/workspaces`, ...);
see `../core/README.md` for the API itself.

## Setup

```bash
npm install
cp .env.example .env.local   # adjust VITE_CORE_API_URL if core isn't on :8824
```

## Running

```bash
npm run dev
```

Requires `core` running locally (`go run .` in `../core`, or the built
binary) with `WORKSPACE_ROOT` set, and its `CORS_ALLOWED_ORIGINS` including
this dev server's origin — `http://localhost:5173` is core's default, which
matches Vite's default dev port, so no extra config is needed for a default
local setup.

## Building

```bash
npm run build   # type-checks then builds to dist/
```

## Environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `VITE_CORE_API_URL` | `http://localhost:8824` | Base URL of the `core` API this UI calls. |
