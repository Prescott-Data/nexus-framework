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

## Connection IDs in agent deployments

Your application stores connection IDs and passes them to agents at task dispatch time. A well-designed agent integration keeps connection ID management out of agent code. The agent receives the connection ID as task input, uses it to fetch credentials, and does not store it beyond the current task.

If your agent system uses a task queue or orchestrator, inject the relevant connection IDs into the task payload when the task is enqueued. The agent retrieves them from the payload, fetches credentials at the start of execution, and proceeds.

---

## MCP Server Integration

For building MCP servers that automatically resolve and inject tokens, see the dedicated [MCP Server Integration Guide](mcp-integration.md).
