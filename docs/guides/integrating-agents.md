---
icon: material/robot-love-outline
---

# Integrating Agents

This guide covers the two ways an agent retrieves credentials from Nexus at runtime: the Bridge library for Go agents, and the manual HTTP flow for agents written in other languages or for cases where you want direct control over credential retrieval.

---

## The Bridge library (Go)

The Bridge is the recommended integration path for Go agents. It handles everything after the OAuth handshake: authenticating requests, maintaining persistent connections through token rotations, and retrying on transient failures.

Import the library and instantiate it with a Gateway client:

```go
import (
    "context"
    "net/http"

    bridge "nexus.io/nexus-bridge"
    "nexus.io/nexus-bridge/telemetry"
    oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

func main() {
    authClient := oauthsdk.New("http://nexus-gateway.example.com")

    agentLabels := map[string]string{"agent_id": "my-agent"}
    b := bridge.NewStandard(authClient, agentLabels)

    http.Handle("/metrics", telemetry.Handler())
    go http.ListenAndServe(":9090", nil)

    connectionID := "conn_01HXYZ..."
    endpointURL := "wss://external.service.com/stream"

    b.MaintainWebSocket(context.Background(), connectionID, endpointURL, &myHandler{})
}
```

`MaintainWebSocket` runs a loop. When the current access token approaches expiry, the Bridge fetches a new one from the Gateway and seamlessly re-authenticates the connection without interrupting your handler. Exponential backoff handles transient network failures.

The `agentLabels` map is applied as constant labels to all Prometheus metrics the Bridge emits. This allows you to filter Bridge metrics by agent in your observability stack.

### gRPC connections

For gRPC, use `MaintainGRPC` instead of `MaintainWebSocket`. The API is the same: you provide a `connection_id`, a target endpoint, and a handler. The Bridge injects the strategy-appropriate credentials as gRPC metadata headers on the initial connection and on each re-authentication.

---

## Sidecar proxy integration

Use the Sidecar when an agent is not written in Go and should not receive raw provider tokens. The agent calls the local proxy with a connection ID, while the sidecar fetches credentials from the Gateway, applies the configured auth strategy, and forwards the request upstream.

```python
import os
import requests

resp = requests.get(
    "http://localhost:8070/user/repos",
    headers={
        "X-Nexus-Provider": "github",
        "X-Nexus-Connection-ID": os.environ["NEXUS_CONNECTION_ID"],
    },
    timeout=30,
)
resp.raise_for_status()
```

See [The Sidecar](../concepts/sidecar.md) for route configuration and operations details.

---
## Manual HTTP integration

Use this approach if your agent is not written in Go, or if you want explicit control over when credentials are fetched rather than having the Bridge manage them.

### Fetching credentials

Call `GET /v1/token/{connection_id}` on the Gateway:

```bash
curl -s http://nexus-gateway.example.com/v1/token/conn_01HXYZ...
```

The response includes a `strategy` field and a `credentials` object. The strategy tells you how to use the credentials:

```json
{
  "strategy": { "type": "oauth2" },
  "credentials": {
    "access_token": "ya29.A0AfH...",
    "expires_at": 1715000000
  },
  "expires_at": 1715000000
}
```

For `oauth2`, set `Authorization: Bearer <access_token>` on your outgoing request.

For `basic_auth`, the credentials object contains `username` and `password`. Encode them as Base64 and set `Authorization: Basic <encoded>`.

For `api_key`, the credentials object contains the key fields defined by the provider's schema. The schema tells you the field name and where to inject it (header, query parameter, or request body).

### When to re-fetch

The `expires_at` field is a Unix timestamp. Fetch a new token before this time. A safe strategy is to re-fetch when the remaining lifetime drops below five minutes. Do not cache a token beyond its expiry. The Broker runs the background refresh loop, so a fresh fetch is always cheap and always returns a valid token.

### Using the Go SDK directly

If your agent is Go but you want explicit fetches rather than automatic management, use the SDK:

```go
import (
    "context"
    oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

client := oauthsdk.New("https://nexus-gateway.example.com")

payload, err := client.GetToken(context.Background(), "conn_01HXYZ...")
if err != nil {
    return err
}

strategyType := payload.Strategy["type"]
```

Inspect `strategyType` and use the `payload.Credentials` map to extract the values you need. See [Client Libraries](../concepts/client-libraries.md) for the full SDK method reference.

### Using the TypeScript SDK

```typescript
import { NexusClient } from '@prescott/nexus-sdk';

const client = new NexusClient({ gatewayUrl: 'https://nexus-gateway.example.com' });

const token = await client.getTokenByConnectionId('conn_01HXYZ...');
// Apply based on strategy type
if (token.strategy.type === 'oauth2') {
    headers['Authorization'] = `Bearer ${token.credentials.access_token}`;
}
```

### Using the Python SDK

```python
from nexus_sdk import NexusClient, NexusClientOptions

client = NexusClient(NexusClientOptions(gateway_url='https://nexus-gateway.example.com'))

token = client.get_token_by_connection_id('conn_01HXYZ...')
if token.strategy['type'] == 'oauth2':
    headers['Authorization'] = f"Bearer {token.credentials['access_token']}"
```

---

## Managing Connection Identifiers

A common and critical misconception when integrating with Nexus is treating the `connection_id` as a permanent, immutable identifier for a user's integration (like a foreign key in your database). 

In reality, a `connection_id` acts more like a specific "session" or "instance" of a connection. Nexus handles identity and connections differently than you might expect, particularly around re-authorization.

### The Re-Authorization Lifecycle

If a background token refresh fails permanently (for example, if the user manually revokes your app's access in their Google settings, or the refresh token expires), Nexus places the connection into an `attention_required` state.

To resolve this, the user must re-authenticate. When you initiate a new OAuth handshake by calling `RequestConnection`, **Nexus generates a brand-new `connection_id`**. 

When the user completes the flow on the provider's site, Nexus redirects them back to your `return_url` with this newly minted `connection_id`. If your backend ignores this and continues requesting tokens using the old `connection_id` (which is permanently stuck in `attention_required`), your token requests will fail.

Because of this lifecycle, you have two options for managing connection identifiers:

---

### Option 1: Workspace & Provider Resolution (Recommended)

Instead of manually tracking and updating `connection_id`s in your own database, the most robust approach is to let Nexus do the tracking. Nexus allows you to dynamically resolve the active token using just the `workspace_id` and the `provider_name`.

This endpoint automatically queries the database for the most recently created, `active` connection for that workspace and provider, abstracting away the underlying `connection_id` entirely.

**Using the REST API:**
```bash
curl -s "http://nexus-gateway.example.com/connections/resolve?workspace_id=tenant_123&provider_name=salesforce"
```

**Using the TypeScript SDK:**
```typescript
const token = await client.getTokenByWorkspaceAndProvider('tenant_123', 'salesforce');
```

**Handling Multiple Users per Workspace (B2B architectures)**

Nexus intentionally does not have a native `user_id` column. The `workspace_id` acts as the sole tenant identifier. How you use this depends on your app's architecture:

- **1 User = 1 Workspace (B2C):** If your application provides a personal workspace for every user, simply map your platform's `user_id` directly to the Nexus `workspace_id`.
- **N Users = 1 Workspace (B2B):** If your platform has multiple users inside a single workspace, and each user needs their *own* distinct connection to the same provider (e.g., their personal Slack account), querying by just the workspace ID will fail (it will just return the token for the last user who authenticated). 
  
  To solve this, **concatenate the tenant and user IDs** when initiating the connection flow:
  ```typescript
  // When initiating the connection:
  const consentUrl = await client.requestConnection({
      workspaceId: `tenant_123:user_456`, 
      providerId: 'google-prod',
      returnUrl: 'https://myapp.com/callback'
  });
  
  // When fetching the token later:
  const token = await client.getTokenByWorkspaceAndProvider(`tenant_123:user_456`, 'google-prod');
  ```
  This creates a unique namespace for every user within a workspace, allowing Nexus to resolve their active connections without you ever needing to save a `connection_id`.

---

### Option 2: Tracking the Connection ID Manually

If you prefer to store the `connection_id` in your own platform's database (e.g., mapping it to an `integrations` table) and passing it to agents at task dispatch time, you must strictly follow these rules:

1. **Update on Re-Auth:** You **must** update the `connection_id` in your database every time a user completes an OAuth flow. When the user is redirected to your `return_url`, extract the new `connection_id` from the query parameters and overwrite the old one in your database:
   ```typescript
   // Example Express.js callback handler
   app.get('/callback', async (req, res) => {
       const newConnectionId = req.query.connection_id;
       const status = req.query.status; // "success"
       
       // CRITICAL: Overwrite the old connection ID for this user
       await db.integrations.update({
           where: { userId: req.user.id, provider: 'salesforce' },
           data: { nexusConnectionId: newConnectionId }
       });
       
       res.send("Integration successful!");
   });
   ```

2. **Agent Isolation:** A well-designed agent integration keeps connection ID management entirely out of the agent's code. Your application orchestrator should retrieve the current `connection_id` from your database and inject it into the agent's task payload. The agent uses it to fetch credentials, executes the task, and then discards it.

---

## MCP Server Integration

For building MCP servers that automatically resolve and inject tokens, see the dedicated [MCP Server Integration Guide](mcp-integration.md).
