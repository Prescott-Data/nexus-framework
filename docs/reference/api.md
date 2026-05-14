# API Reference

Nexus exposes two API surfaces: the **Gateway API**, which agents and applications call, and the **Broker API**, which is internal and used only by the Gateway and administrative tooling.

The full OpenAPI 3.0 specification is at [`openapi.yaml`](https://github.com/Prescott-Data/nexus-framework/blob/main/openapi.yaml). The v1 surface is frozen — no breaking changes without a major version bump and a deprecation period.

## Gateway API

The stable, public-facing surface for all agent integrations. Versioned at `/v1`.

### Connection endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/request-connection` | Initiate an OAuth handshake or static credential capture |
| `GET` | `/v1/check-connection/{id}` | Poll connection status |
| `GET` | `/v1/token/{id}` | Retrieve credentials for an active connection |
| `POST` | `/v1/refresh/{id}` | Force a token refresh for a connection |
| `GET` | `/v1/capture-schema` | Fetch the credential schema for a static provider |
| `POST` | `/v1/capture-credential` | Submit credentials for a static provider |

### Agent session endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/agent-sessions` | Request a scoped session for a registered agent |
| `GET` | `/v1/agent-sessions/{id}` | Check session status and metadata |
| `DELETE` | `/v1/agent-sessions/{id}` | Close a session and revoke the token |
| `POST` | `/v1/agent-sessions/obo` | Request an OBO session tied to a user context token |

### Provider endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/providers` | List all registered providers |
| `GET` | `/v1/providers/metadata` | Grouped metadata for frontend rendering |
| `POST` | `/v1/providers` | Register a new provider |
| `GET` | `/v1/providers/{id}` | Get provider by ID |
| `PATCH` | `/v1/providers/{id}` | Update specific fields of a provider |
| `DELETE` | `/v1/providers/{id}` | Delete a provider |

### System

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/health` | Health check — returns `200 OK` when ready |

### Authentication

Connection and token endpoints do not require authentication headers. Access control is enforced by the `connection_id` — only callers holding a valid connection ID can retrieve its credentials. Treat `connection_id` values as session tokens.

Provider management and agent session endpoints require the `X-API-Key` header.

### Token response shape

`GET /v1/token/{id}` returns a structured payload regardless of credential type:

```json
{
  "strategy": { "type": "oauth2" },
  "credentials": {
    "access_token": "eyJ...",
    "expires_at": 1715000000
  },
  "scope": "openid email profile",
  "expires_at": 1715000000
}
```

The `strategy.type` field tells you how to apply the credentials. See [Authentication Strategies](../concepts/auth-strategies.md) for all strategy types and their credential shapes.

---

## Broker API

Internal surface. Called by the Gateway and administrative tooling (`nexus-cli`). Do not expose the Broker to agents or untrusted networks.

The Broker's OpenAPI spec is at [`nexus-broker/openapi.yaml`](https://github.com/Prescott-Data/nexus-framework/blob/main/nexus-broker/openapi.yaml). It evolves between minor versions — fields and endpoints may change.

### Agent registry endpoints (admin)

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/v1/agents` | Register an agent with its allowed scopes |
| `GET` | `/admin/v1/agents` | List all registered agents |
| `GET` | `/admin/v1/agents/{id}` | Get a specific agent |
| `PATCH` | `/admin/v1/agents/{id}` | Update agent allowed scopes |
| `DELETE` | `/admin/v1/agents/{id}` | Deregister an agent |

### Audit

| Method | Path | Description |
|---|---|---|
| `GET` | `/audit` | Query audit log events |

All Broker API calls require the `X-API-Key` header. See the [Audit Log](audit-log.md) guide for query parameters and response schema.
