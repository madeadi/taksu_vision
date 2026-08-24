Every service shall:
1. Do a specific task
2. Pure service without side effects
3. Read and write files only inside a workspace: every path-shaped parameter
   (`images_dir`, `crops_out_dir`, `json_output_path`, `image_path`,
   `pdf_path`, `crops_dir`, `pages_out_dir`, ...) is relative to a
   `workspace_id`, resolved against `$WORKSPACE_ROOT/{workspace_id}/files/`.
   Every `/tasks` call requires `workspace_id`. `WORKSPACE_ROOT` must be set
   to the same shared-disk path on every service process (see `core/`).
   `json_output_path`, when given, is where the service also writes its json
   response — as a workspace-relative path like every other path param.

Service shall implements the following endpoints:
- GET /health
- POST /tasks

The one exception is `core/`, which owns workspace lifecycle
(create/upload/download/list/delete, with a 7-day TTL) rather than a single
compute task — it implements `GET /health` plus a small CRUD REST API
instead of `POST /tasks`. See `core/README.md`.

Service shall output a response with the following data structure: 
- input
- output
- start_at
- finished_at
- success: bool
