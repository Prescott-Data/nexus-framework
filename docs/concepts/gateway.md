# The Gateway

The Gateway is the public API for Nexus. Your application backend and agents communicate exclusively with the Gateway. The Broker is unreachable from outside.

## APIs

The Gateway ships two binaries from the same OpenAPI spec:

| Binary | Protocol | Use case |
|---|---|---|
| `nexus-rest` | HTTP/1.1 REST | Default. Works with any HTTP client. |
| `nexus-grpc` | gRPC / HTTP/2 | High-concurrency agents that benefit from multiplexing. |

## Endpoints

### Provider management

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/providers` | Register a new provider profile |
| `GET` | `/v1/providers` | List all providers |
| `GET` | `/v1/providers/{id}` | Get a provider by ID |
| `PATCH` | `/v1/providers/{id}` | Update provider fields |
| `DELETE` | `/v1/providers/{id}` | Delete a provider |

### OAuth consent flow

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/request-connection` | Initiate OAuth consent — returns `auth_url` and `connection_id` |
| `GET` | `/v1/callback` | OAuth redirect target — proxied to the Broker |
| `GET` | `/v1/capture-schema` | Get credential field schema for static providers |
| `POST` | `/v1/capture-credential` | Submit static credentials — returns `connection_id` |

### Token operations

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/token/{connection_id}` | Fetch current credentials for a connection |
| `POST` | `/v1/refresh/{connection_id}` | Force an immediate token refresh |
| `GET` | `/v1/check-connection/{connection_id}` | Get connection status |

## Authentication

Pass your Gateway API key in the `X-API-Key` header on every request.

```
X-API-Key: <your-gateway-api-key>
```

The Gateway uses a separate `API_KEY` when forwarding requests to the Broker. These should be different values.

## CORS

CORS is only relevant if your frontend JavaScript calls the Gateway directly during the OAuth consent flow. Configure `ALLOWED_ORIGINS` with your frontend domain. Server-side agents do not need CORS configured.
