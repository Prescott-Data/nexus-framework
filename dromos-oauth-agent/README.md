# Dromos OAuth Handling Agent (OHA)

The OAuth Handling Agent provides a REST interface for other agents to initiate OAuth flows via the Dromos OAuth Broker, verify connection status, and retrieve tokens without storing them.

## Features
- requestConnection: Initiate OAuth via broker `/auth/consent-spec`
- checkConnection: Poll broker for readiness
- getToken: Proxy token retrieval from broker vault (no storage)
- HMAC state verification using shared STATE_KEY
- Logs with sensitive data redaction (planned)

## Configuration
- `PORT` (default `8090`)
- `BROKER_BASE_URL` (e.g. `http://localhost:8080`)
- `STATE_KEY` base64-encoded; must match broker for state validation

## REST Endpoints
- POST `/v1/request-connection`
- GET `/v1/check-connection/{connectionID}`
- GET `/v1/token/{connectionID}`

## Run
```bash
export BROKER_BASE_URL="http://localhost:8080"
export STATE_KEY="$(openssl rand -base64 32)"  # use the broker's key in real env

go run ./cmd/oha
```

## gRPC + HTTP Gateway (optional)
- Proto: `api/proto/oha/v1/oha.proto` (with HTTP annotations)
- Build gRPC variant: `go build -tags grpc ./...`
- Codegen via buf (once installed):
```bash
buf generate
```
- OpenAPI spec is emitted to `gen/openapi`.

### Run the gRPC + Gateway server
```bash
# generate code first (requires buf installed)
buf generate

# environment
export BROKER_BASE_URL="http://localhost:8080"
export STATE_KEY="$(openssl rand -base64 32)"  # use broker's key in prod
export PORT_GRPC=9090
export PORT_HTTP=8090

# build and run with grpc tag
go build -tags grpc -o bin/oha-grpc ./cmd/oha-grpc
./bin/oha-grpc

# gRPC at localhost:${PORT_GRPC}
# HTTP gateway (JSON/REST) at localhost:${PORT_HTTP}
```
