---
icon: material/account-key-outline
---

# Agent Sessions

This guide covers the complete developer journey for building an agent that uses Nexus for authentication — from registering providers and agents to making scoped requests and handling the full OBO flow.

## What you are building

An agent that calls Salesforce with read-only access, calls Google Calendar with read-only access, and executes internal business operations only when a human user with the right permission triggers them. No OAuth code in the agent. No credentials in environment variables. No refresh token logic. Nexus handles all of it.

## Step 1 — Register your providers (one-time admin)

```bash
# Salesforce
curl -X POST https://your-gateway.example.com/v1/providers \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "salesforce",
    "auth_type": "oauth2",
    "client_id": "SF_CLIENT_ID",
    "client_secret": "SF_CLIENT_SECRET",
    "auth_url": "https://login.salesforce.com/services/oauth2/authorize",
    "token_url": "https://login.salesforce.com/services/oauth2/token",
    "scopes": ["crm:contacts:read", "crm:contacts:write"],
    "params": { "skip_scope_on_exchange": true }
  }'

# Google Calendar
curl -X POST https://your-gateway.example.com/v1/providers \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "google-calendar",
    "auth_type": "oauth2",
    "client_id": "GOOGLE_CLIENT_ID",
    "client_secret": "GOOGLE_CLIENT_SECRET",
    "issuer": "https://accounts.google.com",
    "enable_discovery": true,
    "scopes": ["https://www.googleapis.com/auth/calendar.events.readonly"]
  }'
```

## Step 2 — Register your agents (one-time admin)

Each agent is registered with its maximum allowed scope set. An agent can never request more than what is declared here.

```bash
# CRM agent — read-only access to Salesforce contacts
curl -X POST https://your-gateway.example.com/admin/v1/agents \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "crm-agent",
    "description": "Reads customer records from Salesforce",
    "allowed_scopes": ["crm:contacts:read"]
  }'

# Calendar agent
curl -X POST https://your-gateway.example.com/admin/v1/agents \
  -H "X-API-Key: your-api-key" \
  -d '{
    "agent_id": "calendar-agent",
    "description": "Reads calendar events",
    "allowed_scopes": ["https://www.googleapis.com/auth/calendar.events.readonly"]
  }'

# Ops agent — custom scopes for internal operations
curl -X POST https://your-gateway.example.com/admin/v1/agents \
  -H "X-API-Key: your-api-key" \
  -d '{
    "agent_id": "ops-agent",
    "description": "Executes authorized internal financial operations",
    "allowed_scopes": ["acme:gliding", "acme:flaring"]
  }'
```

## Step 3 — Connect users to providers

Each user who will authorize an agent must complete the OAuth flow for their account. Your backend initiates this:

```bash
curl -X POST https://your-gateway.example.com/v1/request-connection \
  -H "X-API-Key: your-api-key" \
  -d '{
    "workspace_id": "user_sarah",
    "provider_id": "SALESFORCE_PROVIDER_UUID",
    "scopes": ["crm:contacts:read", "crm:contacts:write"],
    "return_url": "https://your-app.com/connections/callback"
  }'
```

Redirect the user to the returned `auth_url`. After they authorize, poll `check-connection` until `status` is `active`. Store the `connection_id` against the user.

## Step 4 — Agent requests a scoped session

When the agent needs to call Salesforce for a user, it requests a session. In Go:

```go
session, err := nexusClient.RequestAgentSession(ctx, oauthsdk.AgentSessionInput{
    AgentID:      "crm-agent",
    ProviderName: "salesforce",
    Scopes:       []string{"crm:contacts:read"},
    TTL:          15 * time.Minute,
})
if err != nil {
    return err
}
defer nexusClient.CloseAgentSession(ctx, session.SessionID)

resp, err := http.Get("https://api.salesforce.com/v1/contacts?q=" + filter,
    // session.AccessToken is scoped to crm:contacts:read only
    withBearer(session.AccessToken),
)
```

In Python:

```python
session = nexus.request_agent_session(
    agent_id="crm-agent",
    provider="salesforce",
    scopes=["crm:contacts:read"],
)
try:
    resp = httpx.get(
        "https://api.salesforce.com/v1/contacts",
        params={"q": filter},
        headers={"Authorization": f"Bearer {session.access_token}"},
    )
    return resp.json()["records"]
finally:
    nexus.close_agent_session(session.session_id)
```

In TypeScript:

```typescript
const session = await nexus.requestAgentSession({
  agentId: 'crm-agent',
  provider: 'salesforce',
  scopes: ['crm:contacts:read'],
  ttl: 900,
})
try {
  const resp = await fetch('https://api.salesforce.com/v1/contacts', {
    headers: { Authorization: `Bearer ${session.accessToken}` },
  })
  return resp.json()
} finally {
  await nexus.closeAgentSession(session.sessionId)
}
```

## Step 5 — OBO session for user-triggered operations

When a human user triggers an operation that requires their specific authorization, use an OBO session. The Broker validates the user's permissions before issuing the session.

```python
def run_gliding(user_token: str, customer_ids: list) -> dict:
    # Raises NexusAuthError if the user does not have acme:gliding permission
    obo = nexus.request_obo_session(
        agent_id="ops-agent",
        provider="internal-ops",
        scopes=["acme:gliding"],
        user_context_token=user_token,
    )
    try:
        return internal_ops.glide(
            customer_ids=customer_ids,
            tenant_id=obo.tenant_id,        # enforced downstream
            clearance_level=obo.clearance_level,
        )
    finally:
        nexus.close_agent_session(obo.session_id)
```

The Broker validates both gates before issuing the OBO session:
- The user must have `acme:gliding` in their permissions (verified via your `BACKEND_AUTH_URL`)
- The `ops-agent` must have `acme:gliding` in its `allowed_scopes`

If either check fails, the request returns `403`.

## What the agent never wrote

| Responsibility | Who handles it |
|---|---|
| OAuth implementation | Broker |
| Refresh token logic | Broker |
| Token storage | Broker |
| Scope enforcement | Broker — at session request time |
| JWT validation for OBO | Broker — calls your backend, extracts claims |
| User context stamping | Broker — stamps `acting_for`, `tenant_id`, `clearance_level` |
| Credential rotation | Broker |
| Token expiry | Broker — session has explicit `expires_at` |

## Session vs connection — when to use which

| Use connection tokens (`GetToken`) | Use agent sessions (`RequestAgentSession`) |
|---|---|
| Agent has no registered identity | Agent is registered with `allowed_scopes` |
| You want maximum flexibility | You want enforced least-privilege |
| Bridge-based persistent connections | Discrete per-operation credential grants |
| Existing integrations | New agent builds |
