# Dromos OAuth Gateway

The OAuth Gateway exposes a gRPC API (primary) with an HTTP JSON gateway (REST) for convenience. It initiates OAuth flows via the Dromos OAuth Broker, verifies connection status, and retrieves tokens without storing them.

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

## Agent Integration (summary)
- Agents should store only `connection_id`.
- Flow:
  1) `POST /v1/request-connection` → returns `authUrl` + `connection_id`; send user to `authUrl`.
  2) After callback, poll `GET /v1/check-connection/{connection_id}` until `active`.
  3) Fetch tokens on-demand: `GET /v1/token/{connection_id}`; use `access_token` for provider API calls.
- Refresh: For now, use Broker `POST /connections/{connection_id}/refresh` with `X-API-Key`. We will add a gateway proxy endpoint.

See `INTEGRATIONS.md` for full details.

## Run (gRPC primary + REST gateway)
```bash
# optional: buf generate   (only if you change protos)
export BROKER_BASE_URL="http://localhost:8080"
export STATE_KEY="$(openssl rand -base64 32)"  # use the broker's key in real env
export PORT_GRPC=9090
export PORT_HTTP=8090

go build -tags grpc -o bin/oauth-gateway-grpc ./cmd/oha-grpc
./bin/oauth-gateway-grpc
```

## REST-only (optional)
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

# run the REST-only server (no gRPC)
go run ./cmd/oha

# gRPC at localhost:${PORT_GRPC}
# HTTP gateway (JSON/REST) at localhost:${PORT_HTTP}
```
