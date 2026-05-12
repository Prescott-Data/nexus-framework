# Nexus Feature Proposal: Agent Auth as a First-Class Citizen

**Author:** Prescott Data  
**Branch:** `docs/agent-auth-proposal`  
**Status:** Proposal — open for discussion  
**Target:** nexus-framework maintainers

---

## Context

Nexus today is a well-built OAuth 2.0 broker and gateway. It handles user consent flows, encrypts tokens at rest using AES-GCM, manages token refresh, and exposes a clean REST API with stable v1 contracts. The Go SDK and bridge components are solid infrastructure.

**But there is a gap.**

The framework was designed around a user-centric model: a human user connects to a provider (Google, Salesforce, GitHub), the broker stores the token, and the agent retrieves it. The agent is a consumer of user-delegated tokens.

This model works for a narrow class of agent use cases. It does not cover the full auth surface of a production agent system — and that surface is where developers are spending the most painful hours.

This document proposes a set of concrete, backward-compatible additions to the nexus-framework that would make it the complete auth infrastructure for developers building AI agents. We ground every proposal in a real developer experience we have lived through, and we include accurate code showing both where the pain sits today and what the solution looks like.

---

## The Developer Story

This is not a hypothetical. This is the experience of a developer building agents that interact with external services and internal business operations.

### Act 1: "I need OAuth for my Salesforce tool"

A developer writes a tool their agent will call:

```python
# tools/salesforce.py
def get_customers(filter: str) -> list[Customer]:
    client_id = os.getenv("SALESFORCE_CLIENT_ID")        # 😬
    client_secret = os.getenv("SALESFORCE_CLIENT_SECRET") # 😬 agent can read this
    token = oauth_exchange(client_id, client_secret, ...)  # 😬 they wrote this
    return salesforce_api.get("/contacts", token=token, filter=filter)
```

The credentials are in the environment. The agent has access to the environment. The agent can read `SALESFORCE_CLIENT_ID` and `SALESFORCE_CLIENT_SECRET`. If the agent is compromised, so are the credentials.

### Act 2: "Now I need Google Calendar too"

```python
# tools/calendar.py
def get_invites(since: datetime) -> list[Event]:
    # Another OAuth implementation
    # Another refresh token loop
    # Another token storage mechanism
    # Another set of environment variables
    ...
```

Two providers. Two OAuth stacks. Two refresh loops. Two credential stores. The developer is doing the same work twice — and they know there will be more providers.

### Act 3: "What if the agent wipes my Salesforce ERP?"

The agent has a Salesforce token. That token was issued with `crm:contacts:read crm:contacts:write crm:delete`. The developer gave it broad scopes because the OAuth setup was painful and they didn't want to do it again. Now they are worried.

There is no mechanism in their current setup to say "this agent can read contacts but can never delete." The token is the token.

### Act 4: "We have internal operations — gliding and flaring"

The business has internal processing operations:
- **Gliding**: a specific data transformation, authorized only for the finance team
- **Flaring**: a pipeline trigger, authorized only for the ops team

The developer wants an agent to perform these operations — but only when a human user who has the appropriate permission triggers the mission. The agent should never be able to glide or flare autonomously, and never on behalf of a user who doesn't have that permission.

This is the On-Behalf-Of (OBO) problem. The developer has to:
1. Validate the user's JWT
2. Extract their role and permissions
3. Check if they have the required permission
4. Stamp those values onto the agent session
5. Enforce them in every downstream operation for the life of the mission

This is non-trivial. Most developers either skip it (insecure) or build it ad hoc (fragile).

### Act 5: "I heard about Nexus"

The developer finds the nexus-framework. They deploy the broker. They register their providers. They get tokens. But:

- The broker gives them the token. It doesn't restrict which agent can use which token.
- The broker has no concept of agent identity.
- The broker has no concept of custom (non-OAuth) scopes.
- The broker has no OBO mechanism.
- The SDK is Go. The agent is Python.

The framework solves the n+1 OAuth problem but leaves the developer with half an auth story.

**This proposal describes the other half.**

---

## What Nexus Is Today

```
User-centric OAuth flow:
  user → OAuth consent → broker stores token → agent calls GetToken(connection_id) → uses token

Components:
  nexus-broker    Go service, Postgres + Redis, handles OAuth flows and token storage
  nexus-gateway   Go reverse proxy, stable v1 API
  nexus-sdk       Go library, HTTP client for the gateway
  nexus-bridge    Go library, persistent WebSocket/gRPC connection manager with auto token refresh
  nexus-cli       CLI tooling
```

This is solid. We are not proposing to change it.

---

## What Nexus Needs to Become

```
Complete agent auth surface:

  OAuth layer (exists):
    user → connects Google/Salesforce → broker holds token → agent gets token via gateway

  Agent session layer (proposed):
    agent presents identity → broker validates scopes → issues 15-minute scoped session
    human triggers agent → OBO validates user permission → session stamped with user context

  SDK layer (partially exists):
    Go SDK: exists, extend with agent session methods
    Python SDK: exists inside jarviscore, needs extracting and publishing to PyPI
    TypeScript SDK: does not exist, needs building
```

---

## Proposal 1: Agent Identity and Session Endpoints

### Problem
The broker has no concept of an agent as a principal. Agents are invisible in the current model. There is no way to say "agent X is allowed scopes Y and Z" or to enforce that at token issuance time.

### Proposed additions to nexus-broker

**New database tables:**
```sql
-- Agent registry
CREATE TABLE agents (
    id          TEXT PRIMARY KEY,           -- e.g. "crm-agent", "calendar-agent"
    description TEXT,
    allowed_scopes TEXT[] NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ DEFAULT now(),
    active      BOOLEAN DEFAULT true
);

-- Agent sessions (ephemeral, short-lived)
CREATE TABLE agent_sessions (
    session_id      TEXT PRIMARY KEY,       -- e.g. "sess_abc123"
    agent_id        TEXT REFERENCES agents(id),
    connection_id   TEXT,                   -- the underlying broker connection
    scopes_granted  TEXT[] NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    closed_at       TIMESTAMPTZ,
    -- OBO fields
    obo             BOOLEAN DEFAULT false,
    acting_for      TEXT,                   -- user_id from validated JWT
    tenant_id       TEXT,
    clearance_level INT DEFAULT 1
);
```

**New endpoints:**

```
# Admin — register agent identity
POST /admin/v1/agents
{
  "agent_id": "crm-agent",
  "description": "Reads and updates customer records",
  "allowed_scopes": ["crm:contacts:read", "crm:contacts:write"]
}
→ 201 { "agent_id": "crm-agent", "allowed_scopes": [...] }

# Admin — list agents
GET /admin/v1/agents
→ 200 { "agents": [...] }

# Agent — request scoped session
POST /v1/agent-sessions
{
  "agent_id": "crm-agent",
  "provider_name": "salesforce",
  "scopes": ["crm:contacts:read"],
  "ttl_seconds": 900
}
→ 201 {
    "session_id": "sess_a1b2c3",
    "access_token": "eyJ...",
    "scopes_granted": ["crm:contacts:read"],
    "expires_at": "2025-05-11T21:00:00Z"
  }
→ 403 if "crm:contacts:read" not in crm-agent.allowed_scopes
→ 403 if "crm:contacts:write" requested but only "crm:contacts:read" registered

# Agent — close session
DELETE /v1/agent-sessions/{session_id}
→ 200 { "status": "closed" }

# Agent — check session
GET /v1/agent-sessions/{session_id}
→ 200 { "session_id": "...", "expires_at": "...", "active": true }
```

### Enforcement logic

When an agent requests a session:
1. Look up `agent_id` in the agents table
2. Validate every requested scope against `allowed_scopes`
3. If any requested scope is not in the allowed list → 403
4. Look up the underlying `connection_id` for `provider_name`
5. Issue the token with only the requested scopes
6. Create an `agent_sessions` record with `expires_at = now() + ttl_seconds`

The agent never gets more than it explicitly requests, and can never request more than was registered.

---

## Proposal 2: On-Behalf-Of (OBO) Sessions

### Problem
When a human user triggers an agent mission, the agent should act with the user's delegated permissions — not with blanket agent permissions. The agent should never autonomously perform operations that require human authorization.

This pattern is needed for:
- Operations scoped to a user's org/tenant
- Operations requiring specific user roles ("only finance can glide")
- Full audit trails showing which human user authorized which agent action

### Proposed endpoint

```
POST /v1/agent-sessions/obo
{
  "agent_id": "ops-agent",
  "provider_name": "internal-ops",
  "scopes": ["acme:gliding"],
  "user_context_token": "<JWT signed by your auth backend>",
  "ttl_seconds": 900
}
```

### OBO flow

```
1. Broker calls BACKEND_AUTH_URL/auth/verify-agent-token with { "token": user_context_token }
2. Backend returns { "user_id": "sarah@acme.com", "tenant_id": "acme-finance", "clearance_level": 2, "permissions": ["acme:gliding", "acme:reporting"] }
3. Broker checks: is "acme:gliding" in sarah's permissions?
   → No  → 403 "user not authorized for scope acme:gliding"
   → Yes → continue
4. Broker checks: is "acme:gliding" in ops-agent's allowed_scopes?
   → No  → 403 "scope acme:gliding not permitted for agent ops-agent"
   → Yes → continue
5. Broker issues session stamped with user context:
```

```json
{
  "session_id": "obo_x9y8z7",
  "access_token": "eyJ...",
  "scopes_granted": ["acme:gliding"],
  "expires_at": "2025-05-11T21:00:00Z",
  "obo": true,
  "acting_for": "sarah@acme.com",
  "tenant_id": "acme-finance",
  "clearance_level": 2
}
```

The agent uses `acting_for`, `tenant_id`, and `clearance_level` to enforce data isolation in every downstream operation. The agent never decodes the original JWT — all trust decisions live in the broker.

### Environment configuration

```bash
# Required for OBO to function
BACKEND_AUTH_URL=https://your-backend.com
# Broker calls: POST $BACKEND_AUTH_URL/auth/verify-agent-token
# Expected response: { user_id, tenant_id, clearance_level, permissions[] }
```

---

## Proposal 3: Custom Scopes

### Problem
Not every permission maps to an OAuth provider scope. Organizations define internal operations with their own names: `acme:gliding`, `acme:flaring`, `reports:generate`, `pipeline:trigger`. These need to be first-class citizens in the authorization system, not workarounds.

### Proposed additions

Custom scopes are declared when registering an agent. No separate scope registry is required — the agent's `allowed_scopes` list is the source of truth.

```bash
# Register an agent with a mix of provider scopes and custom scopes
curl -X POST http://localhost:8080/admin/v1/agents \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "ops-agent",
    "description": "Performs internal financial operations on user delegation",
    "allowed_scopes": [
      "acme:gliding",
      "acme:flaring",
      "crm:contacts:read"
    ]
  }'
```

Custom scopes follow the same enforcement as provider scopes. The distinction is in how the broker resolves the session token:

- **Provider scope** (`crm:contacts:read`): broker fetches the underlying OAuth token from the stored connection and returns it scoped to the requested permissions
- **Custom scope** (`acme:gliding`): broker returns a signed session token asserting the agent is authorized for this scope — the downstream system validates against this assertion

For custom scopes, the response looks like:

```json
{
  "session_id": "sess_xyz",
  "session_token": "eyJ...",
  "scopes_granted": ["acme:gliding"],
  "expires_at": "2025-05-11T21:00:00Z",
  "token_type": "session"
}
```

The downstream service verifies the session token against the broker's public key (or via `GET /v1/agent-sessions/{id}` for simplicity).

---

## Proposal 4: Vault Integration (Broker Should Not Store Client Secrets)

### Problem
The broker currently stores OAuth `client_id` and `client_secret` in Postgres. While these are encrypted at rest, this creates a competing concern with existing secret management infrastructure that most teams already have (HashiCorp Vault, AWS Secrets Manager, GCP Secret Manager, Okta).

Nexus should never try to replace these tools. It should integrate with them.

### Proposed vault adapters

Add a `SECRET_BACKEND` configuration option to the broker:

```bash
# Option A: Store in broker's Postgres (current behavior, default)
SECRET_BACKEND=internal

# Option B: HashiCorp Vault
SECRET_BACKEND=hashicorp-vault
VAULT_ADDR=https://vault.acme.com
VAULT_TOKEN=s.xxxxx
VAULT_MOUNT=secret
VAULT_PATH_PREFIX=nexus/providers

# Option C: AWS Secrets Manager
SECRET_BACKEND=aws-secrets-manager
AWS_REGION=us-east-1
AWS_SECRET_PREFIX=nexus/providers

# Option D: GCP Secret Manager
SECRET_BACKEND=gcp-secret-manager
GCP_PROJECT_ID=acme-prod
GCP_SECRET_PREFIX=nexus-providers
```

When `SECRET_BACKEND` is set to anything other than `internal`:
- Provider registration (`POST /providers`) stores only metadata in Postgres (provider name, auth URLs, scopes) — not credentials
- The `client_id` and `client_secret` are written to the configured vault backend at the path `<prefix>/<provider_name>/client_id` and `<prefix>/<provider_name>/client_secret`
- At OAuth flow time, the broker reads credentials from the vault backend
- Nexus stores only the tokens it produces — never the input credentials

This makes Nexus a pure orchestration layer: it performs OAuth flows using credentials from your vault, and stores the resulting tokens. It never competes with your existing secret management.

---

## Proposal 5: Multi-Language SDK Support

### Problem
Agent developers are overwhelmingly Python and TypeScript. The Go SDK (nexus-sdk) is valuable for Go infrastructure but does not serve the majority of the target audience.

### Python SDK

The Python SDK already exists inside `jarviscore` as `jarviscore.nexus.NexusClient`. It wraps the same gateway API. It needs to be:

1. Extracted into a standalone repository or `nexus-sdk-python/` directory
2. Published to PyPI as `nexus-sdk`
3. Extended with the new agent-session methods

**Target interface:**

```python
from nexus import NexusClient

nexus = NexusClient(gateway_url="https://nexus-gateway.acme.com")

# Standard agent session
session = nexus.request_agent_session(
    agent_id="crm-agent",
    provider="salesforce",
    scopes=["crm:contacts:read"],
    ttl=900
)
# session.access_token  → ready to use
# session.expires_at    → datetime
# session.scopes_granted → list of granted scopes

# Use the token
response = httpx.get(
    "https://api.salesforce.com/v1/contacts",
    headers={"Authorization": f"Bearer {session.access_token}"}
)

# Close when done
nexus.close_agent_session(session.session_id)

# OBO session
obo = nexus.request_obo_session(
    agent_id="ops-agent",
    provider="internal-ops",
    scopes=["acme:gliding"],
    user_context_token=request.headers["X-User-Token"]
)
# obo.acting_for      → "sarah@acme.com"
# obo.tenant_id       → "acme-finance"
# obo.clearance_level → 2
```

**Installation:**
```bash
pip install nexus-sdk
```

### TypeScript SDK

New package. Same REST API, TypeScript ergonomics.

**Target interface:**

```typescript
import { NexusClient } from 'nexus-sdk'

const nexus = new NexusClient({ gatewayUrl: 'https://nexus-gateway.acme.com' })

// Standard agent session
const session = await nexus.requestAgentSession({
  agentId: 'crm-agent',
  provider: 'salesforce',
  scopes: ['crm:contacts:read'],
  ttl: 900,
})

// Use the token
const response = await fetch('https://api.salesforce.com/v1/contacts', {
  headers: { Authorization: `Bearer ${session.accessToken}` }
})

// Close when done
await nexus.closeAgentSession(session.sessionId)

// OBO session
const obo = await nexus.requestOBOSession({
  agentId: 'ops-agent',
  provider: 'internal-ops',
  scopes: ['acme:gliding'],
  userContextToken: req.headers['x-user-token'],
})
// obo.actingFor, obo.tenantId, obo.clearanceLevel
```

**Installation:**
```bash
npm install nexus-sdk
```

### Go SDK extension

Extend the existing `nexus-sdk/client.go` with agent session methods:

```go
// New types in nexus-sdk/agent.go

type AgentSessionInput struct {
    AgentID      string        `json:"agent_id"`
    ProviderName string        `json:"provider_name"`
    Scopes       []string      `json:"scopes"`
    TTL          time.Duration `json:"-"`
}

type AgentSession struct {
    SessionID     string    `json:"session_id"`
    AccessToken   string    `json:"access_token"`
    ScopesGranted []string  `json:"scopes_granted"`
    ExpiresAt     time.Time `json:"expires_at"`
}

type OBOSessionInput struct {
    AgentID          string        `json:"agent_id"`
    ProviderName     string        `json:"provider_name"`
    Scopes           []string      `json:"scopes"`
    UserContextToken string        `json:"user_context_token"`
    TTL              time.Duration `json:"-"`
}

type OBOSession struct {
    AgentSession
    OBO            bool   `json:"obo"`
    ActingFor      string `json:"acting_for"`
    TenantID       string `json:"tenant_id"`
    ClearanceLevel int    `json:"clearance_level"`
}

// New methods on Client

func (c *Client) RequestAgentSession(ctx context.Context, in AgentSessionInput) (*AgentSession, error)
func (c *Client) RequestOBOSession(ctx context.Context, in OBOSessionInput) (*OBOSession, error)
func (c *Client) CloseAgentSession(ctx context.Context, sessionID string) error
func (c *Client) GetAgentSession(ctx context.Context, sessionID string) (*AgentSession, error)
```

---

## Complete Developer Journey After These Changes

### Setup (one-time, admin)

```bash
# 1. Deploy nexus
docker-compose up -d

# 2. Register providers (credentials go here — never in agent code)
curl -X POST http://localhost:8080/providers \
  -H "X-API-Key: $API_KEY" \
  -d '{
    "profile": {
      "name": "salesforce",
      "auth_type": "oauth2",
      "client_id": "SF_CLIENT_ID",
      "client_secret": "SF_CLIENT_SECRET",
      "auth_url": "https://login.salesforce.com/services/oauth2/authorize",
      "token_url": "https://login.salesforce.com/services/oauth2/token",
      "scopes": ["crm:contacts:read", "crm:contacts:write"]
    }
  }'

curl -X POST http://localhost:8080/providers \
  -H "X-API-Key: $API_KEY" \
  -d '{
    "profile": {
      "name": "google-calendar",
      "auth_type": "oauth2",
      "client_id": "GOOGLE_CLIENT_ID",
      "client_secret": "GOOGLE_CLIENT_SECRET",
      "scopes": ["calendar.events.readonly"]
    }
  }'

# 3. Register agents with their allowed scopes
curl -X POST http://localhost:8080/admin/v1/agents \
  -H "X-API-Key: $API_KEY" \
  -d '{"agent_id": "crm-agent", "allowed_scopes": ["crm:contacts:read"]}'

curl -X POST http://localhost:8080/admin/v1/agents \
  -H "X-API-Key: $API_KEY" \
  -d '{"agent_id": "calendar-agent", "allowed_scopes": ["calendar.events.readonly"]}'

curl -X POST http://localhost:8080/admin/v1/agents \
  -H "X-API-Key: $API_KEY" \
  -d '{"agent_id": "ops-agent", "allowed_scopes": ["acme:gliding", "acme:flaring"]}'
```

### Agent tool code (Python)

```python
# tools/salesforce.py
from nexus import NexusClient
import httpx

nexus = NexusClient(gateway_url="https://nexus-gateway.acme.com")

def get_customers(filter: str) -> list[dict]:
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

```python
# tools/calendar.py
def get_invites(since: str) -> list[dict]:
    session = nexus.request_agent_session(
        agent_id="calendar-agent",
        provider="google-calendar",
        scopes=["calendar.events.readonly"],
    )
    try:
        resp = httpx.get(
            "https://www.googleapis.com/calendar/v3/events",
            params={"timeMin": since},
            headers={"Authorization": f"Bearer {session.access_token}"},
        )
        return resp.json()["items"]
    finally:
        nexus.close_agent_session(session.session_id)
```

```python
# tools/ops.py
def run_gliding(user_token: str, customer_ids: list[str]) -> dict:
    # OBO: user must have acme:gliding permission or this raises NexusAuthError
    obo = nexus.request_obo_session(
        agent_id="ops-agent",
        provider="internal-ops",
        scopes=["acme:gliding"],
        user_context_token=user_token,
    )
    try:
        # Full audit context available
        print(f"Running gliding for {obo.acting_for} (tenant: {obo.tenant_id})")
        return internal_ops.glide(
            customer_ids=customer_ids,
            tenant_id=obo.tenant_id,
            clearance_level=obo.clearance_level,
        )
    finally:
        nexus.close_agent_session(obo.session_id)
```

### What the developer never wrote

```
✅ OAuth implementation         — broker handles it
✅ Refresh token logic          — broker handles it
✅ Token storage                — broker handles it
✅ Scope enforcement            — broker enforces at session request time
✅ JWT validation for OBO       — broker calls backend, extracts claims
✅ User context stamping        — broker stamps acting_for, tenant_id, clearance_level
✅ Credential rotation          — broker handles it
✅ Token expiry                 — session has explicit expires_at, broker enforces
```

---

## Implementation Priority

| Priority | Work item | Effort |
|---|---|---|
| 1 | Broker: `/admin/v1/agents` + `/v1/agent-sessions` endpoints | Medium |
| 2 | Broker: `/v1/agent-sessions/obo` endpoint + JWT validation | Medium |
| 3 | Python SDK: extract from jarviscore, publish to PyPI, add agent session methods | Small |
| 4 | TypeScript SDK: new package, publish to npm | Medium |
| 5 | Broker: vault backend integration (HashiCorp, AWS SSM, GCP) | Large |
| 6 | Go SDK: add `RequestAgentSession`, `RequestOBOSession` to nexus-sdk | Small |
| 7 | OpenAPI spec: document all new endpoints | Small |
| 8 | CLI: `nexus agents list`, `nexus sessions list`, `nexus agents register` | Medium |

---

## What We Are Not Proposing

- ❌ A new standalone service (nexus-authz, nexus-registry, etc.) — all additions go into nexus-broker
- ❌ Competing with HashiCorp Vault, AWS SSM, or GCP Secret Manager — Proposal 4 makes Nexus a consumer of those, not a replacement
- ❌ An agent runtime or orchestration layer — Nexus stays infrastructure

---

## Why This Matters for the OSS Project

The developer building AI agents is the fastest-growing segment of the software market right now. Every agent that connects to an external service — Salesforce, Gmail, Slack, Notion, GitHub — hits the same auth wall. Every team building multi-agent systems where human users can delegate tasks hits the OBO wall. Every organization that wants to define what their agents can and cannot do hits the custom scope wall.

Nexus already solves the hardest infrastructure problem (OAuth broker, encrypted token storage, refresh, stable API). The additions in this proposal are thin layers on top of what already exists. But they complete the story — and they make Nexus the answer to every auth question an agent developer has, not just the easy ones.

The alternative is that developers keep building xxx-registry-style components in every new project, in every language, with varying degrees of correctness. We have been that developer. We built that thing. This proposal is the result of learning from that experience.

---

## References

- [BACKEND_NEXUS_REQUIREMENTS.md](../BACKEND_NEXUS_REQUIREMENTS.md) — OBO protocol specification used in production

---

*This proposal was written by a team that has built and run these patterns in production. We are happy to collaborate on implementation, review PRs, and share the reference implementation code.*
