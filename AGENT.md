# Services
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

## Endpoints

Service shall implements the following endpoints:
- GET /health
- POST /tasks

Services can also implement other endpoints.

The one exception is `core/`, which owns workspace lifecycle
(create/upload/download/list/delete, with a 7-day TTL) rather than a single
compute task — it implements `GET /health` plus a small CRUD REST API
instead of `POST /tasks`. See `core/README.md`.

Service's task shall output a response with the following data structure: 
- input
- output
- start_at
- finished_at
- success: bool

## Web App
Every service shall also have a web app that can be used to:
1. User to try to call the service
2. Manage the service's configuration
3. View the service's logs
4. Perform service's related functions, e.g. train a model. 

The web app address will be registered to the core and will be rendered with iframe by the core's web app.

Service's web app can have any stack, as long as it can be rendered with iframe. If it's with js, The suggested stack for the web app is: 
- React
- Typescript
- Tailwind
- Shadcn
- Typescript

Service's web app can have menu on the top. 
It's preferred to serve at `/web`. 

## Core

It will be used to manage the services. Services will be registered with the core. It will register: 
- URL of the API. It will be assumed that the health endpoint is at `/health`.
- URL of the web app

# Golang
These are the instructions for golang based code:
- For configuration, use viper
- Provide endpoints to refresh the config once the yaml file is updated.
- create config.example.yaml file with the environment variables required by the service.
- don't commit config.yaml.
