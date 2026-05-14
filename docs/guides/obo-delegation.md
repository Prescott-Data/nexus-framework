---
icon: material/account-arrow-right-outline
---

# OBO Delegation

On Behalf Of (OBO) delegation is a session pattern for multi-agent systems where an agent needs to act with a specific user's authorization rather than with system-level access. OBO sessions tie the agent's credentials to the identity and permission tier of the user who initiated the operation.

This matters in multi-tenant systems where agents serve multiple users and where the data a user can access varies by their role or clearance level.

## The problem OBO solves

An agent that holds a system-level credential can operate on any user's behalf. If the agent is compromised or makes an error, there is no connection between what the agent accessed and what the user who triggered the operation was actually permitted to see or modify.

Consider: an agent executing `acme:gliding` — a finance operation that only members of the finance team are authorized to trigger. Without OBO, if any user can trigger the agent, the agent can run `acme:gliding` regardless of whether the triggering user has finance permissions. The agent's authorization is entirely separate from the user's.

OBO sessions solve this by validating the user's permissions before issuing the session. The session is stamped with the user's identity and permission context. The agent can only do what the specific user who triggered it is authorized to do.

## How OBO validation works

OBO uses a two-gate enforcement model. Both gates must pass before a session is issued.

**Gate 1 — Agent scope check**

The requested scope must be in the agent's registered `allowed_scopes`. If `ops-agent` does not have `acme:gliding` in its allowed scopes, the request is rejected with `403` regardless of the user's permissions.

**Gate 2 — User permission check**

The Broker calls your backend's token verification endpoint to validate the user context token and extract the user's permissions:

```
POST {BACKEND_AUTH_URL}/auth/verify-agent-token
{ "token": "<user_context_token>" }
```

Your backend responds with the user's identity and permissions:

```json
{
  "user_id": "sarah@acme.com",
  "tenant_id": "acme-finance",
  "clearance_level": 2,
  "permissions": ["acme:gliding", "acme:reporting"]
}
```

If the requested scope is not in the user's `permissions` array, the Broker rejects the request with `403`. If both gates pass, the Broker issues an OBO session stamped with the user context.

## Configuration

Set `BACKEND_AUTH_URL` on the Broker. The Broker appends `/auth/verify-agent-token` to this URL when validating user context tokens.

```bash
BACKEND_AUTH_URL=https://your-backend.example.com
```

Your backend must implement `POST /auth/verify-agent-token` and return the response shape above. The Broker does not validate the user token itself — that is your backend's responsibility. The Broker trusts your backend's response.

## Creating an OBO session

```bash
curl -X POST https://your-gateway.example.com/v1/agent-sessions/obo \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "ops-agent",
    "provider_name": "internal-ops",
    "scopes": ["acme:gliding"],
    "user_context_token": "<token from your IdP>",
    "ttl_seconds": 900
  }'
```

Response:

```json
{
  "session_id": "obo_x9y8z7",
  "access_token": "eyJ...",
  "scopes_granted": ["acme:gliding"],
  "expires_at": "2026-05-12T21:00:00Z",
  "obo": true,
  "acting_for": "sarah@acme.com",
  "tenant_id": "acme-finance",
  "clearance_level": 2
}
```

## Using the OBO context

The `acting_for`, `tenant_id`, and `clearance_level` fields are for your application to use — they are not enforced downstream by Nexus. Use them to scope data access in your internal services:

```python
obo = nexus.request_obo_session(
    agent_id="ops-agent",
    provider="internal-ops",
    scopes=["acme:gliding"],
    user_context_token=user_token,
)
try:
    result = internal_ops.glide(
        customer_ids=customer_ids,
        tenant_id=obo.tenant_id,            # isolate to user's tenant
        clearance_level=obo.clearance_level, # enforce level in downstream logic
    )
    audit_log.write(
        operation="acme:gliding",
        actor=obo.acting_for,               # full audit trail
        tenant=obo.tenant_id,
    )
    return result
finally:
    nexus.close_agent_session(obo.session_id)
```

## The clearance level is fixed at session creation

The clearance level cannot be changed after a session is issued. An agent that received a clearance level 2 session cannot escalate to clearance level 3 resources within that session. The Broker enforces this at issuance time.

## Audit trail

Every OBO session is written to `audit_events` with the full delegation chain: the agent ID, the user the agent is acting for, the tenant, the scopes granted, the clearance level, and the session timestamps. This gives your compliance team a complete record of every user-triggered agent operation without requiring agent-level instrumentation.

## Error cases

| Error | Cause |
|---|---|
| `403 scope_not_permitted_for_agent` | Requested scope is not in `ops-agent.allowed_scopes` |
| `403 user_not_authorized_for_scope` | User's permissions do not include the requested scope |
| `503 backend_auth_unavailable` | `BACKEND_AUTH_URL` is unreachable or returned a non-200 response |
| `401 invalid_user_context_token` | Your backend returned an error for the provided token |
