<p align="center">
  <a href="https://github.com/Prescott-Data/nexus-framework">
    <img src="docs/assets/nexus-logo-blue.png" width="180px" alt="Nexus — auth infrastructure for autonomous agents">
  </a>
</p>

<p align="center">
  <strong>One authority for every service your agents touch — OAuth 2.0/OIDC brokering, encrypted token custody, scope-ceilinged agent identity, and a tamper-evident audit trail. Your agents never write auth code or hold a raw secret. Go, TypeScript, Python — or no SDK at all.</strong>
</p>

<p align="center">
  <a href="https://nexus.developers.prescottdata.io/">Docs</a>
  ·
  <a href="https://nexus.developers.prescottdata.io/getting-started/quickstart/">Quickstart</a>
  ·
  <a href="https://discord.gg/AbskSXypq">Discord</a>
  ·
  <a href="https://developers.prescottdata.io/blog">Blog</a>
</p>

<p align="center">
  <a href="https://github.com/Prescott-Data/nexus-framework/stargazers">
    <img src="https://img.shields.io/github/stars/Prescott-Data/nexus-framework?style=flat-square" alt="GitHub Repo stars">
  </a>
  <a href="https://github.com/Prescott-Data/nexus-framework/tags">
    <img src="https://img.shields.io/github/v/tag/Prescott-Data/nexus-framework?style=flat-square&label=version&color=1758F5" alt="Latest version">
  </a>
  <a href="https://github.com/Prescott-Data/nexus-framework/issues">
    <img src="https://img.shields.io/github/issues/Prescott-Data/nexus-framework?style=flat-square" alt="GitHub issues">
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/github/license/Prescott-Data/nexus-framework?style=flat-square" alt="License: Apache 2.0">
  </a>
  <a href="https://nexus.developers.prescottdata.io/">
    <img src="https://img.shields.io/badge/docs-nexus-blue?style=flat-square" alt="Docs">
  </a>
</p>

> Nexus is an open-source, provider-agnostic credential broker for AI agents and services. It gives every agent in your fleet one authority for OAuth 2.0 / OIDC connections, token lifecycle, agent identity, scoped sessions, and on-behalf-of delegation — so your agents never write auth code or hold a raw secret.

- **Nexus Broker**: The authority. Holds all master secrets encrypted at rest and runs the token refresh loop. Never exposed to agents.
- **Nexus Gateway**: The public API. Agents ask it for short-lived credentials; it proxies to the Broker over an internal channel.

```text
                          your agents
     Go SDK · TypeScript SDK · Python SDK · Bridge (Go, WS/gRPC)
                  · any language via the Sidecar ·
                               │
                               │  short-lived credentials only —
                               │  never a master secret
                               ▼
    ┌─────────────────────────────────────────────────────────┐
    │                      Nexus Gateway                      │
    │      the public API — agents never reach the Broker     │
    └────────────────────────────┬────────────────────────────┘
                                 │  internal channel
                                 ▼
    ┌─────────────────────────────────────────────────────────┐
    │                       Nexus Broker                      │
    │                                                         │
    │    master tokens encrypted at rest · refresh loop       │
    │    agent identity · scope ceilings · scoped sessions    │
    │    on-behalf-of delegation · tamper-evident audit       │
    └────────────────────────────┬────────────────────────────┘
                                 │  OAuth 2.0 / OIDC
                                 ▼
             GitHub · Notion · Google · Slack · your IdP
```

## Table of Contents

- [Why Nexus?](#why-nexus)
- [Getting Started](#getting-started)
  - [1. Run the Stack](#1-run-the-stack)
  - [2. Critical Configuration](#2-critical-configuration)
  - [3. Connect an Agent](#3-connect-an-agent)
- [Key Features](#key-features)
- [Understanding the Components](#understanding-the-components)
- [Client SDKs](#client-sdks)
- [Examples](#examples)
  - [The OAuth Consent Flow](#1-the-oauth-consent-flow)
  - [MCP Server with Automatic Token Injection](#2-mcp-server-with-automatic-token-injection)
  - [Zero-Code Auth with the Sidecar](#3-zero-code-auth-with-the-sidecar)
  - [Persistent Connections with the Bridge](#4-persistent-connections-with-the-bridge)
- [When to Use Nexus](#when-to-use-nexus)
- [Documentation](#documentation)
- [Contribution](#contribution)
- [License](#license)
- [Frequently Asked Questions (FAQ)](#frequently-asked-questions-faq)

## Why Nexus?

Every agent that connects to an external service hits the same wall: OAuth flows, token refresh loops, credential rotation, and per-provider auth quirks — all written from scratch, per integration, per team. Nexus eliminates that wall:

- **Register once, connect everywhere**: Register a provider once; every agent in your fleet connects through a single authority.
- **Agents never hold durable secrets**: The Broker encrypts master tokens at rest and issues only short-lived credentials on demand. A compromised agent yields nothing durable.
- **Identity and scope ceilings**: Agents are registered principals with declared scope ceilings — a `crm-agent` registered with `crm:contacts:read` cannot request `crm:delete`, even if the underlying connection has that scope.
- **On-behalf-of delegation**: When a human triggers an agent mission, the Broker validates the user's permission and stamps the session with their identity and tenant context.
- **Tamper-evident audit trail**: Every credential request, session open, and session close is logged.
- **Polyglot by design**: Go, TypeScript, and Python SDKs plus a language-agnostic Sidecar proxy — full feature parity across all of them.

## Getting Started

Follow the full walkthrough in the [quickstart guide](https://nexus.developers.prescottdata.io/getting-started/quickstart/), or get running locally in a few minutes:

### 1. Run the Stack

The fastest way to start is with Docker Compose. This spins up the Broker, Gateway, Sidecar, Postgres, and Redis:

```bash
# 1. Configure environment
cp .env.example .env

# 2. Start the stack
make up

# Or if you don't have make:
docker-compose up -d --build
```

- **Broker**: http://localhost:8080
- **Gateway**: http://localhost:8090
- **Sidecar**: http://localhost:8070
- **Admin API Key**: Configured in `.env` (Default: `nexus-admin-key`)

### 2. Critical Configuration

⚠️ Nexus requires two primary shared secrets to operate securely:

1. **`ENCRYPTION_KEY`**: A 32-byte key used by the Broker to encrypt tokens at rest.
2. **`STATE_KEY`**: A 32-byte key shared between the Broker and Gateway to sign and verify the OAuth `state` parameter.

**Both services will refuse to start if these variables are missing or invalid.** In distributed deployments, the `STATE_KEY` **must** be identical across all Broker and Gateway instances, or OAuth callbacks will fail with "Invalid state" errors.

Generate a secure key with:

```bash
openssl rand -base64 32
```

### 3. Connect an Agent

Install the SDK for your language and initiate your first connection:

```bash
pip install nexus-sdk
```

```python
from nexus_sdk import NexusClient, NexusClientOptions, RequestConnectionInput

client = NexusClient(NexusClientOptions(
    gateway_url='http://localhost:8090',
))

# 1. Initiate OAuth consent
conn = client.request_connection(RequestConnectionInput(
    user_id='user-123',
    provider_name='github',
    scopes=['repo', 'read:user'],
    return_url='https://myapp.com/callback',
))
# → Redirect user to conn.auth_url

# 2. Poll until the user completes consent
status = client.wait_for_active(conn.connection_id)

# 3. Fetch a credential
token = client.get_token_by_connection_id(conn.connection_id)
```

See the [Provider Management Guide](docs/guides/managing-providers.md) for registering identity providers, and the [Agent Integration Guide](docs/guides/integrating-agents.md) for the full agent-side flow.

## Key Features

- **Provider-agnostic OAuth 2.0 / OIDC brokering**: One integration layer for every provider your agents touch — GitHub, Notion, Google, or your own IdP.
- **Encrypted token custody**: Master tokens are encrypted at rest in the Broker; agents receive only short-lived, scoped credentials.
- **Automatic refresh**: A background refresh loop keeps connections alive — agents never see an expired token.
- **Agent identity & scoped sessions**: Registered agent principals, scope ceilings, and per-mission sessions with user and tenant context.
- **Internal business scopes**: First-class scopes like `pipeline:trigger` enforced by the same authority as OAuth tokens.
- **MCP-ready**: Build MCP servers with automatic per-tenant token injection; all SDK logging is stdio-safe.
- **Zero-code auth injection**: The Sidecar proxies plain HTTP from any language and injects credentials before forwarding to allowlisted upstreams.
- **Audit everything**: Tamper-evident logging of every credential request and session event.

## Understanding the Components

Nexus splits the auth surface into a control plane and a data plane:

| Component | Role |
|---|---|
| **[Broker](nexus-broker/README.md)** | The authority. Holds all master secrets encrypted at rest, runs the refresh loop. Never exposed to agents directly. |
| **[Gateway](nexus-gateway/README.md)** | The public API. Agents call the Gateway; it proxies to the Broker over an internal channel. |
| **[Bridge](nexus-bridge/README.md)** | Go library that runs inside your agent process and injects credentials into outgoing HTTP and gRPC requests automatically. |
| **[Sidecar](nexus-sidecar/README.md)** | Polyglot proxy. The agent sends plain HTTP with a connection ID; the Sidecar injects credentials and forwards to an allowlisted upstream. |
| **[CLI](nexus-cli/)** | Command-line tooling for operating a Nexus deployment. |

Start with [Architecture](docs/architecture.md) — it establishes the control/data plane split, the OAuth handshake flow, and the credential retrieval model that every other doc assumes.

## Client SDKs

Connect your application or MCP server to Nexus using the official SDK for your language:

| Language | Package | Install |
|---|---|---|
| **Go** | [`nexus-sdk`](nexus-sdk/README.md) | `go get github.com/Prescott-Data/nexus-framework/nexus-sdk@latest` |
| **TypeScript** | [`@dromos/nexus-sdk`](nexus-sdk-ts/README.md) | `npm install @dromos/nexus-sdk` |
| **Python** | [`nexus-sdk`](nexus-sdk-python/README.md) | `pip install nexus-sdk` |

All SDKs provide full feature parity: connection management, token retrieval, MCP token injection, caching, retry logic, and structured errors.

## Examples

Four patterns cover almost every way agents consume Nexus. Each example below is runnable as-is against a local stack (`make up`).

### 1. The OAuth Consent Flow

Connect a user to a provider once; Nexus custodies the token and keeps it fresh from then on. Same three steps in every SDK — initiate, wait for consent, fetch a credential.

**TypeScript**

```typescript
import { NexusClient } from '@dromos/nexus-sdk';

const client = new NexusClient({ gatewayUrl: 'http://localhost:8090' });

// 1. Initiate OAuth consent
const conn = await client.requestConnection({
  userId:       'user-123',
  providerName: 'github',
  scopes:       ['repo', 'read:user'],
  returnUrl:    'https://myapp.com/callback',
});
// → Redirect user to conn.authUrl

// 2. Poll until the user completes consent
await client.waitForActive(conn.connectionId);

// 3. Fetch a short-lived credential
const token = await client.getTokenByConnectionId(conn.connectionId);
```

**Go**

```go
import (
    "context"
    "time"
    oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

client := oauthsdk.New("http://localhost:8090")

conn, err := client.RequestConnection(ctx, oauthsdk.RequestConnectionInput{
    UserID:       "user-123",
    ProviderName: "github",
    Scopes:       []string{"repo", "read:user"},
    ReturnURL:    "https://myapp.com/callback",
})
// → Redirect user to conn.AuthURL

status, err := client.WaitForActive(ctx, conn.ConnectionID, 1500*time.Millisecond)
token, err := client.GetToken(ctx, conn.ConnectionID)
```

**Python**

```python
from nexus_sdk import NexusClient, NexusClientOptions, RequestConnectionInput

client = NexusClient(NexusClientOptions(gateway_url='http://localhost:8090'))

conn = client.request_connection(RequestConnectionInput(
    user_id='user-123',
    provider_name='github',
    scopes=['repo', 'read:user'],
    return_url='https://myapp.com/callback',
))
# → Redirect user to conn.auth_url

client.wait_for_active(conn.connection_id)
token = client.get_token_by_connection_id(conn.connection_id)
```

### 2. MCP Server with Automatic Token Injection

The pattern for multi-tenant MCP servers: the SDK resolves the right token for each workspace+provider pair, caches it, and injects the `Authorization` header — your tool handlers never touch a token.

```
AI Agent → MCP Server (your code) → Nexus SDK → Nexus Gateway → Upstream API
                                         ↑
                                (resolves & caches token)
```

**TypeScript** — `createFetcher` returns a drop-in `fetch` replacement:

```typescript
import { NexusClient } from '@dromos/nexus-sdk';

const nexus = new NexusClient({ gatewayUrl: process.env.NEXUS_GATEWAY_URL! });

// workspaceId identifies the tenant; provider is "github", "notion", etc.
const fetcher = nexus.createFetcher({ workspaceId: 'workspace-123', provider: 'github' });

// Inside your MCP tool handler — token resolved, cached, and injected automatically:
const resp = await fetcher('https://api.github.com/user/repos?per_page=10&sort=updated');
const repos = await resp.json();
```

**Go** — `AuthenticatedHTTPClient` returns a standard `*http.Client`:

```go
nexus := oauthsdk.New(os.Getenv("NEXUS_GATEWAY_URL"))
cache := oauthsdk.NewTokenCache(30 * time.Second)

// A standard *http.Client with automatic token injection.
gh := nexus.AuthenticatedHTTPClient(cache, "workspace-123", "github")

resp, err := gh.Get("https://api.github.com/user/repos?per_page=10&sort=updated")
```

**Python** — `authenticated_fetch` makes the request with auth headers injected:

```python
from nexus_sdk import NexusClient, NexusClientOptions, TokenCache
import json

client = NexusClient(NexusClientOptions(gateway_url='http://localhost:8090'))
cache = TokenCache()

status, headers, body = client.authenticated_fetch(
    cache, 'workspace-123', 'github',
    'https://api.github.com/user/repos',
    headers={'User-Agent': 'MyApp/1.0'},
)
repos = json.loads(body)
```

> **MCP stdio safety**: all SDK logs go to `stderr`, never `stdout` — the JSON-RPC transport is never corrupted. Full working MCP servers in all three languages are in the [MCP Server Integration guide](docs/guides/mcp-integration.md).

### 3. Zero-Code Auth with the Sidecar

For agents in any language — or where the agent must never see a token at all. The agent sends plain HTTP to the Sidecar with a connection ID; the Sidecar fetches the credential, applies the provider's auth strategy, strips caller-supplied auth headers, and forwards to an allowlisted upstream.

```bash
# Run the sidecar with an upstream allowlist
GATEWAY_BASE_URL=http://localhost:8090 \
NEXUS_ROUTES=github=https://api.github.com \
go run ./cmd/nexus-sidecar
```

```bash
# Path-prefixed routing
curl http://localhost:8070/github/user/repos \
  -H "X-Nexus-Connection-ID: $CONNECTION_ID"

# Or header-based routing
curl http://localhost:8070/user/repos \
  -H "X-Nexus-Provider: github" \
  -H "X-Nexus-Connection-ID: $CONNECTION_ID"
```

The auth engine supports `oauth2`, `basic_auth`, `header`, `query_param`, `hmac_payload`, and `aws_sigv4` strategies. A minimal Python `requests` client is at [nexus-sidecar/examples/python_requests.py](nexus-sidecar/examples/python_requests.py).

### 4. Persistent Connections with the Bridge

Go agents that hold long-lived WebSocket or gRPC connections embed the [Bridge](nexus-bridge/README.md). It authenticates with the strategy returned by the Gateway, proactively refreshes credentials *before* they expire, and reconnects with exponential backoff — with structured logging and Prometheus metrics built in.

```go
import (
    "github.com/Prescott-Data/nexus-framework/nexus-bridge"
    oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

type myWsHandler struct{}
func (h *myWsHandler) OnConnect(send func(message []byte) error) { fmt.Println("Connected!") }
func (h *myWsHandler) OnMessage(message []byte)                   { fmt.Printf("Msg: %s\n", message) }
func (h *myWsHandler) OnDisconnect(err error)                     { fmt.Printf("Disconnected: %v\n", err) }

func main() {
    authClient := oauthsdk.New("http://localhost:8090")

    // Production-ready telemetry: structured logs + Prometheus metrics
    b := bridge.NewStandard(authClient, map[string]string{"agent_id": "my-stable-id"})

    // Auth, proactive refresh, and reconnect with backoff — all handled internally
    err := b.MaintainWebSocket(ctx, "your-connection-id", "wss://external.system.com/ws", &myWsHandler{})
}
```

## When to Use Nexus

Use Nexus when your agents or services need more than a hardcoded API key:

- Multiple agents connecting to multiple OAuth/OIDC providers.
- Multi-tenant systems where credentials must be isolated per workspace or user.
- MCP servers that act on behalf of different tenants.
- Compliance requirements around credential custody, scoping, and audit trails.
- Polyglot fleets where auth logic shouldn't be reimplemented per language.

If you have a single service with a single static API key, you probably don't need Nexus yet.

## Documentation

Full documentation lives at **[nexus.developers.prescottdata.io](https://nexus.developers.prescottdata.io/)**. Key entry points in this repo:

- **[Architecture](docs/architecture.md)**: System overview, components, and data flow.
- **[Deployment & Config](docs/deployment.md)**: How to configure, build, and deploy the services.
- **[SDK Overview](docs/sdks/index.md)**: Choose your SDK and explore the feature matrix.
- **[MCP Server Integration](docs/guides/mcp-integration.md)**: Build MCP servers with automatic token injection.
- **[Agent Integration Guide](docs/guides/integrating-agents.md)**: How to build agents that consume connections.
- **[Provider Management Guide](docs/guides/managing-providers.md)**: How to register and configure identity providers.
- **[API Reference](docs/reference/api.md)**: Links to OpenAPI specifications ([openapi.yaml](openapi.yaml)).
- **[Security Model](docs/reference/security-model.md)**: Security guardrails and hardening.
- **[Nexus Protocol White Paper](docs/WHITE_PAPER.md)**: The design rationale behind the protocol.

## Contribution

Nexus is open-source and we welcome contributions. See [CONTRIBUTING.md](CONTRIBUTING.md) for the full setup guide and PR checklist.

```bash
git clone https://github.com/Prescott-Data/nexus-framework.git
cd nexus-framework
cp .env.example .env
make up

# Run the tests
make test
```

Questions, showcases, and early feature previews live on [Discord](https://discord.gg/AbskSXypq).

## License

Nexus is released under the [Apache 2.0 License](LICENSE).

## Frequently Asked Questions (FAQ)

### Q: What exactly is Nexus?

A: Nexus is a provider-agnostic credential broker for autonomous agents. It centralizes OAuth 2.0 / OIDC connections, token custody, refresh, agent identity, and scoped sessions behind one authority, so agents never write auth code or hold raw secrets.

### Q: Is Nexus a framework for building agents?

A: No — Nexus is the auth layer *underneath* your agents. It works with any agent framework (CrewAI, LangGraph, custom Go/Python/TypeScript services) and with MCP servers.

### Q: Do agents ever see the master OAuth token?

A: No. Master tokens live encrypted in the Broker. Agents receive short-lived credentials via the Gateway, or never see a token at all when using the Sidecar.

### Q: What languages are supported?

A: First-class SDKs for Go, TypeScript, and Python, plus the Sidecar proxy for any language that can speak HTTP.

### Q: Can I use Nexus with MCP servers?

A: Yes — the SDKs provide per-tenant token injection helpers designed for MCP servers, with stdio-safe logging that never corrupts the JSON-RPC transport.

### Q: Is Nexus open-source?

A: Yes, Nexus is open-source under the Apache 2.0 license, and contributions are welcome.
