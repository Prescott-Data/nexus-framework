# Nexus Gateway

The **Nexus Gateway** is the public-facing entry point for the Nexus Framework. It acts as a secure, unified proxy that sits between your Agents (consumers) and the Nexus Broker (system of record).

It allows agents to be "dumb" about the underlying infrastructure while ensuring the sensitive Broker remains isolated from the public internet.

## 1. Core Architecture

The Gateway is built on **Go** and **gRPC**, utilizing the `grpc-gateway` library to serve both REST and gRPC traffic from a single definition.

### Dual-Protocol Support
1.  **gRPC (Port 9090 default):** High-performance, binary protocol used by backend agents and the `nexus-bridge` library.
2.  **REST / JSON (Port 8090 default):** Standard OpenAPI-compliant endpoints used by frontend applications, curl scripts, and non-Go agents.

The Gateway translates incoming REST requests into internal gRPC calls, handles validation, and then proxies valid requests to the Broker.

### The "Thick Proxy" Model
Unlike a simple Nginx reverse proxy, the Gateway contains logic:
- **Identity Abstraction:** It authenticates itself to the Broker using a high-privilege `BROKER_API_KEY`. Agents never possess this key.
- **Protocol Translation:** It converts the Broker's internal REST API responses into the standardized `NexusService` protobuf format.
- **CORS Handling:** It manages Cross-Origin Resource Sharing headers to allow frontend dashboards to safely poll for status.

## 2. API Reference

The Gateway exposes a stable `v1` API.

### `POST /v1/request-connection`
Initiates a new connection flow. This is typically called by your backend to generate an authorization URL for a user.

- **Request Body:**
  ```json
  {
    "user_id": "cust-123",
    "provider_name": "google",
    "scopes": ["email", "profile"],
    "return_url": "https://myapp.com/callback"
  }
  ```
- **Response:**
  ```json
  {
    "connection_id": "con-uuid-888",
    "authUrl": "https://accounts.google.com/..."
  }
  ```

### `GET /v1/check-connection/{connection_id}`
Checks the current status of a handshake. Useful for polling from a frontend while waiting for the user to complete the OAuth flow in a popup.

- **Response:**
  ```json
  { "status": "active" } // or "pending", "failed"
  ```

### `GET /v1/token/{connection_id}`
Retrieves the active credentials for a connection.
- **Behavior:** If the underlying token is expired (or near expiry), the Gateway (via Broker) will attempt to refresh it *before* returning the response.
- **Response:**
  ```json
  {
    "access_token": "ya29...",
    "expires_in": 3599,
    "refresh_token": "..." // Opaque handle
  }
  ```

### `POST /v1/refresh/{connection_id}`
Forces a token refresh with the upstream provider.
- **Use Case:** Call this if your agent receives a `401 Unauthorized` from the external provider, indicating the token was revoked or expired unexpectedly.
- **Response:** Returns the new `TokenResponse`.

## 3. Configuration

The Gateway is configured via environment variables.

| Variable | Required | Default | Description |
| :--- | :--- | :--- | :--- |
| `PORT` | No | `8090` | The HTTP port to listen on. |
| `GRPC_PORT` | No | `9090` | The gRPC port to listen on. |
| `BROKER_BASE_URL` | **Yes** | - | The internal URL of the Nexus Broker (e.g., `http://nexus-broker:8080`). |
| `BROKER_API_KEY` | **Yes** | - | The secret key to authenticate with the Broker. Must match the Broker's `API_KEY`. |
| `STATE_KEY` | **Yes** | - | 32-byte Base64 key for signing state. Must match the Broker's key. |
| `ALLOWED_CIDRS` | No | `0.0.0.0/0` | CIDR allowlist for incoming traffic. |

## 4. Deployment

The Gateway is stateless and can be scaled horizontally.

### Docker Example

```bash
docker run -d \
  -p 8090:8090 \
  -p 9090:9090 \
  -e BROKER_BASE_URL="http://broker:8080" \
  -e BROKER_API_KEY="secret-internal-key" \
  -e STATE_KEY="..." \
  nexus-gateway:latest
```

## 5. Security Context

The Gateway operates in the **Trusted Zone** but faces the **Untrusted Zone** (Public Internet / Agents).

- **Inbound Security:** It relies on `ALLOWED_CIDRS` or an upstream API Gateway (like Kong or AWS API Gateway) to throttle and authenticate agents.
- **Outbound Security:** It uses the `BROKER_API_KEY` to talk to the Broker. This is the only component allowed to talk to the Broker directly.
