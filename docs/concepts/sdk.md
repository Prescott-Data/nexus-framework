# The SDK

The Nexus SDK is a Go HTTP client that wraps the Gateway's REST API. The Bridge uses it internally. Use it directly when you want explicit control over credential fetches rather than the Bridge's automatic lifecycle management.

## Install

```bash
go get github.com/Prescott-Data/nexus-framework/nexus-sdk@v0.1.1
```

## When to use the SDK

- Your agent makes discrete outgoing HTTP calls rather than holding a persistent connection.
- You are building orchestration logic — waiting for a user to complete a consent flow before proceeding.
- You are implementing Nexus in a language without a Bridge. The REST API is accessible from any HTTP client; the SDK is the Go reference implementation.

## Client setup

```go
client := oauthsdk.New(
    "https://your-gateway.internal",
    oauthsdk.WithRetry(oauthsdk.RetryPolicy{
        Retries:    3,
        MinDelay:   200 * time.Millisecond,
        MaxDelay:   5 * time.Second,
        RetryOn429: true,
    }),
    oauthsdk.WithLogger(yourLogger),
)
```

The SDK never logs token bodies.

Retries use exponential backoff with jitter. Retryable statuses: `502`, `503`, `504`, and optionally `429`.

## The agent credential lifecycle

Whether you use the SDK directly or via the Bridge, a Nexus-integrated agent follows five phases:

**1. Resolution** — Fetch the strategy and credentials from the Gateway using the `connection_id`. The response tells you both the credentials and exactly how to apply them to a request.

```go
token, err := client.GetToken(ctx, connectionID)
strategyType := token.Strategy["type"].(string) // "oauth2", "header", "hmac_payload", etc.
```

**2. Configuration** — Apply the strategy to your transport layer. For HTTP, inject the appropriate header or query parameter. For gRPC, inject metadata. The Bridge does this automatically; with the SDK you do it manually using the `strategy` and `credentials` fields.

**3. Maintenance** — Cache the token locally and monitor `ExpiresAt`. Refresh proactively before expiry to avoid latency spikes during active work. Do not wait for a `401` to refresh.

```go
if time.Until(expiresAt) < refreshBuffer {
    token, err = client.RefreshConnection(ctx, connectionID)
}
```

**4. Rotation** — If you receive a `401` from an upstream provider despite a valid token (configuration drift), invalidate your local cache and call `GetToken` again to re-resolve.

**5. Intervention** — If `RefreshConnection` returns `attention_required`, the provider rejected the refresh. The user must re-authorize. Do not retry — surface the need for re-delegation to your application layer.

```go
var e oauthsdk.ErrorEnvelope
if errors.As(err, &e) && e.Code == "attention_required" {
    // initiate a new consent flow for this workspace + provider
}
```

## Methods

| Method | Gateway call | Returns |
|---|---|---|
| `RequestConnection(ctx, input)` | `POST /v1/request-connection` | `auth_url`, `connection_id` |
| `CheckConnection(ctx, id)` | `GET /v1/check-connection/{id}` | Status string |
| `WaitForActive(ctx, id, interval)` | Polls `CheckConnection` | `"active"` or `"failed"` |
| `GetToken(ctx, id)` | `GET /v1/token/{id}` | `TokenResponse` |
| `RefreshConnection(ctx, id)` | `POST /v1/refresh/{id}` | `TokenResponse` |

`WaitForActive` defaults to 1500ms polling interval. Pass zero to use the default.

## TokenResponse fields

| Field | Type | Description |
|---|---|---|
| `AccessToken` | `string` | Short-lived access token (OAuth2 convenience field) |
| `ExpiresAt` | varies | Token expiry — Unix timestamp or RFC3339 depending on provider |
| `Scope` | `*string` | Scope string as returned by the provider |
| `Strategy` | `map[string]interface{}` | How to apply this token to an outgoing request |
| `Credentials` | `map[string]interface{}` | Full credential map — `access_token`, and optionally `refresh_token` |
| `Raw` | `map[string]any` | Raw Gateway JSON — for provider-specific fields not in typed fields |

## Leased identity model

The SDK enforces a strict separation between master secrets and usage secrets. Your agent receives only usage secrets — short-lived access tokens or API keys — never the refresh tokens, client secrets, or signing keys that are held in the Broker's encrypted vault.

If your agent process is compromised, the attacker gets a usage secret valid for at most the remaining token lifetime. They cannot renew it. Once it expires, it is useless.

This is the fundamental difference from static `.env` files: a leaked access token has a bounded blast radius. A leaked refresh token or client secret does not.

## Sidecar model

For environments requiring zero in-process secret exposure, the sidecar deployment model removes credentials from the agent process entirely. The agent sends unauthenticated requests to a local Nexus sidecar on `localhost`. The sidecar fetches credentials from the Gateway, signs the request, and forwards it. The agent process never holds any credential material. This is in development.

## Agent session methods

The following methods are being added to the SDK to support the agent identity model. See [Agent Sessions](../guides/agent-sessions.md) for the full guide.

**Go:**

```go
// Register is done via admin API, not SDK.
// These methods are used at agent runtime:
func (c *Client) RequestAgentSession(ctx context.Context, in AgentSessionInput) (*AgentSession, error)
func (c *Client) RequestOBOSession(ctx context.Context, in OBOSessionInput) (*OBOSession, error)
func (c *Client) CloseAgentSession(ctx context.Context, sessionID string) error
func (c *Client) GetAgentSession(ctx context.Context, sessionID string) (*AgentSession, error)
```

**Python:**

```python
# pip install nexus-sdk
from nexus import NexusClient

nexus = NexusClient(gateway_url="https://your-gateway.example.com")
session = nexus.request_agent_session(agent_id="crm-agent", provider="salesforce", scopes=["crm:contacts:read"])
obo     = nexus.request_obo_session(agent_id="ops-agent", provider="internal-ops", scopes=["acme:gliding"], user_context_token=token)
nexus.close_agent_session(session.session_id)
```

The Python SDK exists today inside `jarviscore` as `jarviscore.nexus.NexusClient`. It is being extracted into a standalone `nexus-sdk` package and published to PyPI.

**TypeScript:**

```typescript
// npm install nexus-sdk
import { NexusClient } from 'nexus-sdk'

const nexus = new NexusClient({ gatewayUrl: 'https://your-gateway.example.com' })
const session = await nexus.requestAgentSession({ agentId: 'crm-agent', provider: 'salesforce', scopes: ['crm:contacts:read'] })
const obo     = await nexus.requestOBOSession({ agentId: 'ops-agent', provider: 'internal-ops', scopes: ['acme:gliding'], userContextToken: token })
await nexus.closeAgentSession(session.sessionId)
```

## Language availability

| Language | Package | Status |
|---|---|---|
| Go | `github.com/Prescott-Data/nexus-framework/nexus-sdk` | Available |
| Python | `nexus-sdk` on PyPI | Extracting from jarviscore — in development |
| TypeScript | `nexus-sdk` on npm | In development |

The REST API is stable and language-agnostic. Any HTTP client works against the Gateway's v1 endpoints.
