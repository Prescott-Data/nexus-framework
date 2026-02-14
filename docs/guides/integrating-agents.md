# Agent Integrations Guide

This guide explains how agents and services integrate with the OAuth framework.

## Recommended Integration: The Bridge Client (for Go)

For Go-based agents and services, the **`bridge` client library** is the recommended integration path. It is a universal connector that handles the entire connection lifecycle, including authentication, polling, refreshing, and reconnection for both **WebSocket** and **gRPC** transports.

Using the Bridge abstracts away the manual HTTP calls detailed below and provides production-ready observability out of the box.

### Example: Persistent WebSocket Connection

```go
import (
	"context"
	"net/http"

	"nexus.io/nexus-bridge"
	"nexus.io/nexus-bridge/telemetry"
	"bitbucket.org/nexus/nexus-framework/nexus-sdk"
)

func main() {
	// 1. Create a client for the Nexus Gateway
	authClient := oauthsdk.New("http://nexus-gateway.example.com")

	// 2. Instantiate the Bridge with standard logging and metrics
	// agentLabels are applied as const_labels to all Prometheus metrics
	agentLabels := map[string]string{"agent_id": "my-stable-id"}
	b := bridge.NewStandard(authClient, agentLabels)
	
	// 3. Expose the /metrics endpoint
	http.Handle("/metrics", telemetry.Handler())
	go http.ListenAndServe(":9090", nil)

	// 4. Run the connection loop
	// The Bridge will fetch the correct credentials (OAuth2, Basic, API Key, etc.)
	// and keep the connection alive indefinitely.
	connectionID := "your-persistent-connection-id"
	endpointURL := "wss://external.service.com/stream"
	
	b.MaintainWebSocket(context.Background(), connectionID, endpointURL, &myAppHandler{})
}
```
See the [`bridge/README.md`](../../bridge/README.md) for full documentation.

---

## Manual HTTP Integration Flow

This flow is for non-Go clients or for understanding the low-level mechanics of the Gateway API. Go clients should prefer the `bridge` library.

### Concepts
- `connection_id`: Opaque handle representing a user-approved connection. Agents store this; do not store tokens.
- Gateway: Front door API for agent access.
- Broker: Handles provider OAuth flows, token storage, and refresh; keep private.

### Typical Flow
1) **Initiate connection (for user-interactive OAuth2):**
   - `POST /v1/request-connection`
   - Body: `{"user_id":"...", "provider_name":"Google", "scopes":[...], "return_url":"..."}`
   - Response: `{"authUrl":"...", "connection_id":"..."}`
   - Redirect the user to `authUrl` to consent.

2) **User completes consent:**
   - The provider redirects to the Broker, which stores tokens and redirects the user to your `return_url` with `status=success&connection_id=...`.

3) **Poll connection status (optional):**
   - `GET /v1/check-connection/{connection_id}` → `{ "status": "active|pending|failed" }`

4) **Use the connection:**
   - `GET /v1/token/{connection_id}`
   - The response is a **generic credential payload**, not just a simple token. You must inspect the `strategy` field to determine how to authenticate.
     ```json
     {
       "strategy": { "type": "oauth2" },
       "credentials": { "access_token": "...", "expires_at": 123456 },
       "expires_at": 123456
     }
     // OR
     {
       "strategy": { "type": "basic_auth" },
       "credentials": { "username": "...", "password": "..." }
     }
     ```

5) **Refresh (until gateway proxy is added):**
   - `POST Broker /connections/{connection_id}/refresh` with header `X-API-Key: <broker_api_key>`.

### Frontend Integration (browser flow)
The browser initiates consent; server(s) hold secrets and call the gateway.

1) **Agent/server asks gateway to create a connection:**
   - Your backend calls `POST /v1/request-connection` with a `return_url` hosted by your frontend (e.g., `https://app.example.com/oauth/return`).

2) **Redirect the user:**
   - From your frontend, redirect the browser to the `authUrl` in the response.

3) **Provider → Broker → Frontend:**
   - After consent, the Broker redirects the user back to your `return_url` with `status=success&connection_id=...`.

4) **Frontend → Backend to consume connection:**
   - Your frontend extracts `connection_id` and sends it to your backend. Your backend stores only the `connection_id`.

5) **Backend fetches credentials on-demand:**
   - Your backend calls Gateway `GET /v1/token/{connection_id}` to retrieve the generic credential payload.

### Using the Go SDK (server-side)
The `nexus-sdk` is a thin client for the Gateway API.

```go
import (
  "context"
  oauthsdk "bitbucket.org/nexus/nexus-framework/nexus-sdk"
)

client := oauthsdk.New("https://<gateway-base-url>")

// Fetch the credential payload:
payload, _ := client.GetToken(context.Background(), "your-connection-id")

// Inspect the strategy to decide how to authenticate
strategyType := payload.Strategy["type"]
```
See the [`nexus-sdk/README.md`](../../nexus-sdk/README.md) for more details.