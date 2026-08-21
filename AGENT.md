Every service shall:
1. Do a specific task
2. Pure service without side effects
3. `json_output_path`: in which the service will also write the json response to this

Service shall implements the following endpoints:
- GET /health
- POST /tasks

Service shall output a response with the following data structure: 
- input
- output
- start_at
- finished_at
- success: bool
