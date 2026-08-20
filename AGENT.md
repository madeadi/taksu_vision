Every service shall:
1. Do a specific task
2. Pure service without side effects
3. Receive workspace so that it can save results to disk

Service shall implements the following endpoints:
- GET /health
- POST /tasks

Service shall output a response with the following data structure: 
- input
- output
- start_at
- finished_at
- success: bool
