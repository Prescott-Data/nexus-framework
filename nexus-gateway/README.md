# Nexus Gateway Service

The **Nexus Gateway** is the public-facing entry point for the Nexus Framework. It acts as a secure, stateless proxy that sits between your Agents (consumers) and the Nexus Broker (system of record).

It exposes a unified API (REST and gRPC) for requesting connections and retrieving credentials, while ensuring the sensitive Broker remains isolated from the public internet.

## Documentation

- **[Architecture & Design](../docs/services/gateway.md)**: Detailed breakdown of the Gateway's role and security model.
- **[API Reference](../docs/reference/api.md)**: Full OpenAPI specification and endpoint details.
- **[Deployment Guide](../docs/deployment.md)**: Production configuration.

## Quick Start (Local)

### 1. Prerequisites
- Go 1.23+
- Running instance of [Nexus Broker](../nexus-broker)

### 2. Configure Environment
Create a `.env` file or export these variables:

```bash
export PORT="8090"
export GRPC_PORT="9090"
export BROKER_BASE_URL="http://localhost:8080" # URL of the internal Broker

# Must match the keys configured in the Broker
export STATE_KEY="<same-base64-key-as-broker>"
export BROKER_API_KEY="nexus-admin-key"
```

### 3. Run the Gateway
```bash
go run ./cmd/nexus-rest
# OR for gRPC
go run ./cmd/nexus-grpc
```

## API Overview

The Gateway serves both HTTP/JSON and gRPC.

| Endpoint | Method | Description |
| :--- | :--- | :--- |
| `/v1/request-connection` | POST | Initiate an OAuth flow. |
| `/v1/check-connection/{id}` | GET | Poll connection status. |
| `/v1/token/{id}` | GET | Retrieve credentials. |
| `/v1/refresh/{id}` | POST | Force a token refresh. |
| `/v1/providers` | GET | List public provider metadata. |

## Development

### Code Generation
The Gateway uses `oapi-codegen` to generate the client for the internal Broker API. If you modify `../nexus-broker/openapi.yaml`, regenerate the client:

```bash
# Install tool
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest

# Generate Client
mkdir -p internal/broker
oapi-codegen -package broker -generate types,client -o internal/broker/client.gen.go ../nexus-broker/openapi.yaml
```

### Build Binaries
```bash
make build-rest
make build-grpc
```
