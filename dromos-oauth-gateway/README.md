# Dromos OAuth Gateway

The **Gateway** is the public-facing entry point for the Dromos OAuth framework. It acts as a stateless proxy and abstraction layer over the internal **Broker**, providing a clean, versioned API for agents and frontend applications.

## Key Responsibilities
*   **Abstraction:** Hides internal Broker APIs and implementation details.
*   **Provider Resolution:** Resolves human-readable provider names (e.g., "google") to internal UUIDs.
*   **State Verification:** Pre-validates the `state` parameter and extracts `connection_id` immediately, enabling easier frontend polling.
*   **Proxy:** Proxies token requests and metadata lookups to the Broker, handling authentication via a shared API Key.

## API Endpoints (HTTP)

### 1. List Providers
Retrieve a grouped list of available integrations and their metadata (Base URLs, scopes, etc.).
```http
GET /v1/providers
```

### 2. Request Connection
Initiate an OAuth flow. Returns the authorization URL and a pre-calculated `connection_id`.
```http
POST /v1/request-connection
{
  "user_id": "workspace-123",
  "provider_name": "google",
  "scopes": ["email", "profile"],
  "return_url": "https://myapp.com/callback"
}
```

### 3. Check Status
Check if a connection is active, pending, or failed.
```http
GET /v1/check-connection/{connection_id}
```

### 4. Get Token
Retrieve the access token for a completed connection.
```http
GET /v1/token/{connection_id}
```

## Development

### Prerequisites
*   Go 1.23+
*   `oapi-codegen` (for regenerating the Broker client)

### Build
```bash
make build-rest   # Build HTTP Gateway
make build-grpc   # Build gRPC Gateway
```

### Run
```bash
# Requires BROKER_BASE_URL and STATE_KEY (must match Broker)
export BROKER_BASE_URL="http://localhost:8080"
export STATE_KEY="<same-base64-key-as-broker>"
export BROKER_API_KEY="<broker-api-key>"

make run-rest
```

### Code Generation
The Gateway uses a generated Go client to talk to the Broker. If the Broker's API changes (and `../dromos-oauth-broker/openapi.yaml` is updated), you must regenerate the client:

```bash
# Install tool if needed
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest

# Generate
mkdir -p internal/broker
oapi-codegen -package broker -generate types,client -o internal/broker/client.gen.go ../dromos-oauth-broker/openapi.yaml
```
