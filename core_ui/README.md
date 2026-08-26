# Taksu Vision Core UI

Workspace management UI for [`core`](../core) — create/list/delete
workspaces, upload files into one, browse and download its files
individually or as a zip archive — plus two pages for
[`detect_boxes`](../detect_boxes): **Try** (run detection over uploaded
images and see the boxes drawn on them) and **Weights** (manage the trained-
weights registry, and train a new model: pick a base weight, annotate raw
images with bounding boxes, then start a training job). React + Vite +
TypeScript + Tailwind CSS + shadcn/ui.

Talks to `core`'s documented REST API (`/health`, `/workspaces`, ...; see
`../core/README.md`) for workspace/file management, and directly to
`detect_boxes`'s REST API (see `../detect_boxes/README.md`) for detection,
the weights registry, and training.

Model selection (Try page) and base-model selection (train wizard) list
weights from `detect_boxes`'s global registry, but `/tasks` and `/train`
only accept a weights path relative to the target workspace's files — a
registered weight is only usable in a workspace if it was trained in that
same workspace. Weights trained elsewhere show as disabled in the wizard's
base-model picker (or fail with a clear error if picked as the Try page's
model), rather than silently being ignored.

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
| `VITE_DETECT_BOXES_API_URL` | `http://localhost:8821` | Base URL of the `detect_boxes` API this UI calls. |
