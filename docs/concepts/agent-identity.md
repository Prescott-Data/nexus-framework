# Agent Identity

The base Nexus connection model is anonymous: any process that holds a `connection_id` can retrieve tokens from it. There is no record of which agent is which, what it is allowed to do, or how long its authorization should last.

The agent identity model adds agents as first-class principals. An agent has a registered identity, a declared set of allowed scopes, and requests short-lived scoped sessions rather than holding a connection token directly. This model is currently in development.

## The agent registry

Each agent is registered once by an administrator:

```bash
curl -X POST https://your-gateway.example.com/admin/v1/agents \
  -H "X-API-Key: your-admin-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "crm-agent",
    "description": "Reads and updates customer records in Salesforce",
    "allowed_scopes": ["crm:contacts:read", "crm:contacts:write"]
  }'
```

| Field | Description |
|---|---|
| `agent_id` | Stable identifier for this agent — used in session requests and audit records |
| `description` | Human-readable label for the agent registry UI |
| `allowed_scopes` | The complete set of scopes this agent is ever permitted to request |

The `allowed_scopes` list is the agent's authorization ceiling. An agent can request any subset of these scopes at session time, but can never request a scope that is not in this list.

## Agent sessions

An agent session is a short-lived, scoped credential grant. The agent requests a session specifying exactly which scopes it needs for the current operation:

```bash
curl -X POST https://your-gateway.example.com/v1/agent-sessions \
  -H "X-API-Key: your-gateway-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "crm-agent",
    "provider_name": "salesforce",
    "scopes": ["crm:contacts:read"],
    "ttl_seconds": 900
  }'
```

Response:

```json
{
  "session_id": "sess_a1b2c3",
  "access_token": "eyJ...",
  "scopes_granted": ["crm:contacts:read"],
  "expires_at": "2026-05-12T21:00:00Z"
}
```

The Broker enforces two constraints before issuing a session:

1. Every requested scope must be in the agent's `allowed_scopes` list.
2. The agent's `allowed_scopes` are themselves a subset of what the underlying connection's provider grants.

If either check fails, the Broker returns `403`. The agent receives exactly what it requests — never more.

## Session lifecycle

| Field | Description |
|---|---|
| `session_id` | Stable identifier — use for audit queries and session closure |
| `access_token` | Short-lived token to use against the provider's API |
| `scopes_granted` | The actual scopes issued — confirm these match what you requested |
| `expires_at` | Hard expiry — the session cannot be refreshed, only replaced |

When the agent finishes its operation, close the session explicitly:

```bash
curl -X DELETE https://your-gateway.example.com/v1/agent-sessions/sess_a1b2c3 \
  -H "X-API-Key: your-gateway-api-key"
```

Closing a session revokes the token server-side. A token intercepted after session closure cannot be replayed.

Sessions that are not explicitly closed expire at `expires_at`. The default TTL is 900 seconds (15 minutes).

## Custom scopes

Not every permission maps to an OAuth provider scope. Internal business operations have their own authorization requirements: `acme:gliding`, `acme:flaring`, `pipeline:trigger`, `reports:generate`. These are custom scopes.

Custom scopes are declared in the agent's `allowed_scopes` list exactly like provider scopes:

```bash
curl -X POST https://your-gateway.example.com/admin/v1/agents \
  -H "X-API-Key: your-admin-api-key" \
  -d '{
    "agent_id": "ops-agent",
    "description": "Performs authorized internal financial operations",
    "allowed_scopes": [
      "acme:gliding",
      "acme:flaring",
      "crm:contacts:read"
    ]
  }'
```

The enforcement mechanism differs from provider scopes:

| Scope type | How the Broker resolves the session token |
|---|---|
| Provider scope (`crm:contacts:read`) | Fetches the underlying OAuth token from the stored connection, returns it scoped to the requested permissions |
| Custom scope (`acme:gliding`) | Returns a signed session token asserting the agent is authorized for this scope — your downstream service validates the assertion |

For custom scopes, the session response includes `token_type: session` rather than `token_type: bearer`:

```json
{
  "session_id": "sess_xyz",
  "session_token": "eyJ...",
  "scopes_granted": ["acme:gliding"],
  "expires_at": "2026-05-12T21:00:00Z",
  "token_type": "session"
}
```

Your downstream service verifies this session token against the Broker's public key, or by calling `GET /v1/agent-sessions/{session_id}` to confirm it is active.

## Why this matters

The connection model does not restrict what an agent can do with a token it retrieves. If the token has `crm:delete` scope, any agent with the `connection_id` can delete records. There is no enforcement point between the token's capabilities and the specific agent's intended use.

The agent identity model enforces least-privilege at the session layer. The `crm-agent` registered with `["crm:contacts:read"]` cannot request `crm:delete` even if the underlying Salesforce connection has it. The agent can only ever operate within its declared scope boundary, regardless of how the connection was authorized.

This is the difference between "the token allows this" and "this agent is allowed to do this."
