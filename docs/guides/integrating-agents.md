# Agent Integration Guide

This guide details how to integrate agents and services with the **Nexus Framework**. It covers the recommended high-level approach using the Bridge library as well as the low-level HTTP API for custom implementations.

## 1. Recommended Integration: The Bridge Library (Go)

For Go-based agents, the **`nexus-bridge`** library is the standard integration path. It provides a production-ready connector that handles the entire connection lifecycle:
- **Authentication:** Automatically fetches and injects credentials (OAuth2, API Keys, etc.).
- **Persistence:** Manages long-lived WebSocket or gRPC connections with automatic reconnection and exponential backoff.
- **Token Management:** Handles token expiration and transparently refreshes credentials via the Gateway.
- **Observability:** Built-in Prometheus metrics and structured logging.

### Installation

```bash
go get github.com/Prescott-Data/nexus-framework/nexus-bridge
go get github.com/Prescott-Data/nexus-framework/nexus-sdk
```

### Example: Persistent WebSocket Connection

This example shows how to maintain a persistent, authenticated WebSocket connection to an upstream provider (e.g., a market data feed) using the Nexus Bridge.

```go
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-bridge"
	"github.com/Prescott-Data/nexus-framework/nexus-bridge/telemetry"
	oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

// myAppHandler processes incoming messages from the WebSocket
type myAppHandler struct{}

func (h *myAppHandler) OnConnect(send func([]byte) error) {
	log.Println("Connected! Sending initialization message...")
	if err := send([]byte(`{"action": "subscribe", "channel": "ticker"}`)); err != nil {
		log.Printf("Failed to send init message: %v", err)
	}
}

func (h *myAppHandler) OnMessage(msg []byte) {
	log.Printf("Received: %s", string(msg))
}

func (h *myAppHandler) OnDisconnect(err error) {
	log.Printf("Disconnected: %v", err)
}

func main() {
	// 1. Configure the Nexus SDK Client
	// This client talks to the Nexus Gateway to fetch and refresh credentials.
	gatewayURL := "http://localhost:8080" // Replace with your Gateway URL
	authClient := oauthsdk.New(gatewayURL)

	// 2. Initialize the Bridge
	// We use NewStandard to get default logging (slog) and Prometheus metrics.
	// agentLabels are attached to all metrics for identification.
	agentLabels := map[string]string{
		"agent_id": "market-data-agent-01",
		"env":      "production",
	}
	b := bridge.NewStandard(authClient, agentLabels)

	// 3. Expose Metrics (Optional but Recommended)
	// The bridge exposes metrics like active_connections, token_refreshes, etc.
	http.Handle("/metrics", telemetry.Handler())
	go func() {
		log.Println("Metrics server listening on :9090")
		if err := http.ListenAndServe(":9090", nil); err != nil {
			log.Fatalf("Metrics server failed: %v", err)
		}
	}()

	// 4. Start the Connection Loop
	// The Bridge will:
	// - Fetch the active token for 'connection-123' from the Gateway.
	// - Connect to 'wss://api.provider.com/feed'.
	// - Automatically refresh the token before it expires.
	// - Reconnect with backoff if the connection drops.
	ctx := context.Background()
	connectionID := "connection-123" // The ID returned by the Request Connection flow
	targetURL := "wss://api.provider.com/feed"

	log.Printf("Starting bridge for connection %s...", connectionID)
	if err := b.MaintainWebSocket(ctx, connectionID, targetURL, &myAppHandler{}); err != nil {
		log.Fatalf("Bridge exited with error: %v", err)
	}
}
```

---

## 2. Low-Level Integration (HTTP API)

If you are not using Go, or need to build a custom integration (e.g., a frontend dashboard or a Python script), you can interact directly with the **Nexus Gateway API**.

### Core Concepts

| Term | Definition |
| :--- | :--- |
| **Connection ID** | An opaque, persistent identifier (UUID) representing a user's authorized link to a provider. Agents store this ID, *never* the actual tokens. |
| **Gateway** | The public-facing API (`nexus-gateway`) that agents interact with. It proxies requests to the internal Broker. |
| **Broker** | The internal service (`nexus-broker`) that manages OAuth flows, stores encrypted tokens, and handles key rotation. |

### The Integration Lifecycle

#### Step 1: Initiate a Connection

Your application (backend or frontend) requests a new connection URL from the Gateway. This is the starting point of the OAuth flow.

**Request:** `POST /v1/request-connection`

```json
{
  "user_id": "user-456",            // Your system's user ID
  "provider_name": "google",        // The provider to connect (must be registered in Nexus)
  "scopes": ["email", "profile"],   // Requested permissions
  "return_url": "https://myapp.com/callback" // Where to redirect the user after consent
}
```

**Response:**

```json
{
  "authUrl": "https://accounts.google.com/o/oauth2/v2/auth?...",
  "connection_id": "con-789",
  "state": "..."
}
```

**Action:** Redirect the user's browser to the `authUrl`.

#### Step 2: User Consent & Callback

1.  The user logs in at the provider (e.g., Google) and grants permission.
2.  The provider redirects the user back to the **Nexus Broker**.
3.  The Broker exchanges the auth code for tokens, encrypts them, and stores them in its database.
4.  The Broker redirects the user to your `return_url` with the status.

**Redirect URL Example:**
`https://myapp.com/callback?status=success&connection_id=con-789&provider=google`

#### Step 3: Check Connection Status

You can verify if a connection is active and ready to use.

**Request:** `GET /v1/check-connection/{connection_id}`

**Response:**

```json
{
  "status": "active" // or "pending", "failed", "attention"
}
```

#### Step 4: Retrieve Credentials

When your agent needs to make a call to the provider, it fetches the current credentials from the Gateway. The Gateway returns a standardized payload regardless of the underlying auth method (OAuth2, API Key, Basic Auth).

**Request:** `GET /v1/token/{connection_id}`

**Response (OAuth2 Example):**

```json
{
  "strategy": {
    "type": "oauth2"
  },
  "credentials": {
    "access_token": "ya29.a0...",
    "expires_at": 1735689600,
    "refresh_token": "1//04..."
  },
  "expires_in": 3599
}
```

**Response (API Key Example):**

```json
{
  "strategy": {
    "type": "header",
    "config": {
      "header_name": "X-API-Key"
    }
  },
  "credentials": {
    "api_key": "sk_live_..."
  }
}
```

**Action:** Your agent uses these credentials to make the actual request to the provider (e.g., adding `Authorization: Bearer <access_token>`).

#### Step 5: Refresh Credentials

Nexus automatically handles token refresh for you.
- **Implicit Refresh:** When you call `GET /v1/token/{connection_id}`, the Broker checks if the token is expired (or near expiry). If so, it attempts to refresh it *before* returning the response.
- **Explicit Refresh:** If you need to force a refresh (e.g., you received a 401 from the provider), call the refresh endpoint.

**Request:** `POST /v1/refresh/{connection_id}`

**Response:** Returns the new token payload (same format as Step 4).

**Error Handling:**
If the refresh fails due to a permanent error (e.g., user revoked access), the Gateway will return a `409 Conflict` with `error: attention_required`. This signals that you must send the user through the "Initiate Connection" flow again.

---

## 3. Frontend Integration (Browser-Based)

For Single Page Applications (SPAs) or client-side integrations:

1.  **Backend Proxy:** It is recommended to have your backend initiate the connection (Step 1) to keep your `return_url` and user context secure.
2.  **Redirect:** Your frontend receives the `authUrl` from your backend and performs `window.location.href = authUrl`.
3.  **Callback:** Handle the callback route (e.g., `/callback`). Extract the `connection_id` and `status` from the query parameters.
4.  **Storage:** Send the `connection_id` to your backend to be associated with the user's account. **Do not store long-lived tokens in local storage.**
5.  **Usage:** When the frontend needs to access data, it should request a short-lived access token from your backend, which proxies the request to the Nexus Gateway.

## 4. API Reference

### Gateway Endpoints

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/v1/request-connection` | Starts a new connection flow. Returns auth URL. |
| `GET` | `/v1/check-connection/{id}` | Returns the current status of a connection. |
| `GET` | `/v1/token/{id}` | Returns the active credentials for a connection. Performs implicit refresh if needed. |
| `POST` | `/v1/refresh/{id}` | Forces a token refresh with the upstream provider. |

### Error Codes

- **200 OK:** Success.
- **400 Bad Request:** Invalid parameters or missing fields.
- **404 Not Found:** Connection ID does not exist.
- **409 Conflict:** `attention_required`. The connection is broken (e.g., token revoked) and requires user intervention (re-consent).
- **500 Internal Server Error:** System error (e.g., database or upstream provider unreachable).
