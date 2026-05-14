# Connections

A connection represents a user's authorization grant to a specific provider. Every token fetch and audit event is tied to a connection ID.

## Fields

| Field | Type | Description |
|---|---|---|
| `id` | UUID | The `connection_id` your application stores and uses for token fetches |
| `workspace_id` | string | Your identifier for the user who authorized this connection — opaque to Nexus |
| `provider_id` | UUID | The provider this connection belongs to |
| `status` | string | Current state — see state machine below |
| `scopes` | `[]string` | The scopes requested for this connection — what you asked for, not necessarily what was granted |
| `code_verifier` | string | PKCE verifier for the OAuth code exchange — removed after completion |
| `return_url` | string | Where the Broker redirects after consent completes |
| `expires_at` | timestamp | Expiry for `pending` connections that are never completed |

## State machine

```
pending ──► active ──► attention
   │                      │
   └──────────────────────┴──► failed
```

| Status | Meaning | Next action |
|---|---|---|
| `pending` | Consent URL issued, user has not authorized yet | Poll `check-connection` or wait for redirect |
| `active` | Token stored — connection is usable | Normal operation |
| `attention` | Token refresh failed with a provider `4xx` | Initiate a new consent flow for this workspace + provider |
| `failed` | Unrecoverable | Delete and recreate |

## Scopes

Nexus has three scope contexts. Confusing them causes subtle bugs.

### Provider scopes

`provider_profiles.scopes` — the default scope list for a provider. The Broker uses these when you call `POST /v1/request-connection` without passing a `scopes` array.

```json
{ "scopes": ["openid", "email", "profile"] }
```

These are configured once on the provider. They represent the broadest set of scopes your application needs from this provider.

### Per-connection scopes

Override the provider defaults for a specific connection by passing `scopes` to `POST /v1/request-connection`:

```json
{
  "workspace_id": "user_abc",
  "provider_id": "3fa85f64-...",
  "scopes": ["read:reports"]
}
```

The Broker stores this array on the connection record and uses it to build the authorization URL: `scope=read:reports`. Two connections to the same provider can carry completely different scopes. Use this to apply least-privilege — a reporting agent gets `["read:reports"]`, an admin agent gets `["read:reports", "write:data", "admin:users"]`.

### Requested vs granted

The scopes your agent requested and the scopes the provider actually granted are not always the same. Providers can downscope a grant based on their own policies or the user's own permission level.

The `scope` field in the token response reflects what was actually granted:

```go
token, _ := client.GetToken(ctx, connectionID)
granted := token.Scope // "read:reports" — even if you requested more
```

Do not assume your agent has a scope because it was requested. If your agent's operations depend on specific scopes, check `token.Scope` after the connection becomes active and surface a clear error if the required scope is absent.

### Provider-specific scope quirks

Some providers handle scopes in non-standard ways. These are controlled via the `params` field on the provider profile:

| Param | Type | Providers | Effect |
|---|---|---|---|
| `skip_scope_on_auth` | bool | Salesforce | Omits `scope` from the authorization URL |
| `skip_scope_on_exchange` | bool | Salesforce | Omits `scope` from the token exchange request |

Both default to `false`. Set them only if the provider rejects requests that include `scope`.

## Static credential connections

For `api_key` and `basic_auth` providers, there is no OAuth redirect. Your backend calls `GET /v1/capture-schema` to get the field schema, presents it to the user, and submits the completed values via `POST /v1/capture-credential`. The connection goes directly to `active`. Static connections cannot be refreshed.

## Token storage

The `tokens` table keeps exactly one row per connection. Every refresh upserts via `ON CONFLICT (connection_id) DO UPDATE`. There is no token history. The current encrypted token is always the only row.
