# Taksu Vision

This is a polyglot monorepo containing Taksu's vision tools. Every folder is a separate service that can be deployed as a standalone service. 

Every service shall:
1. Do a specific task
2. Pure service without side effects
3. Implements shared protobuf

## Technology stack
- Golang
- Java
- Python
- PHP

## Deployment
- Docker

# Service Endpoints

Service shall implements the following endpoints:
- GET /health
- POST /tasks

Service shall output a response with the following data structure: 
- input
- output
- start_at
- finished_at
- success: bool
