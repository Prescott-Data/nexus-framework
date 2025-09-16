## Agent Integrations Guide

This guide explains how agents and services integrate with the OAuth framework using the Gateway (front door) and Broker (backend).

### Concepts
- `connection_id`: Opaque handle representing a user-approved connection. Agents store this; do not store tokens.
- Gateway: Front door API for agent access.
- Broker: Handles provider OAuth flows, token storage, and refresh; keep private.

### Typical Flow
1) Initiate connection (agent → Gateway)
   - POST `oauth-framework/dromos-oauth-gateway` → `/v1/request-connection`
   - Body:
     ```json
     {"user_id":"<workspace-or-user>","provider_name":"Google","scopes":["openid","email","profile"],"return_url":"https://app.example.com/oauth/return"}
     ```
   - Response:
     ```json
     {"authUrl":"...","state":"...","scopes":[...],"provider_id":"...","connection_id":"..."}
     ```
   - Redirect the user to `authUrl` to consent.

2) User completes consent (provider → Broker callback)
   - Broker stores encrypted tokens, marks connection active, and redirects to `return_url?status=success&connection_id=...`.

3) Poll connection status (agent → Gateway)
   - GET `/v1/check-connection/{connection_id}` → `{ "status": "active|pending|failed" }`

4) Use the connection (agent → Gateway)
   - GET `/v1/token/{connection_id}` → token JSON (includes `access_token`, optional `id_token`, `expires_at`).
   - Use `access_token` with the provider API.

5) Refresh (until gateway proxy is added)
   - POST Broker `/connections/{connection_id}/refresh` with header `X-API-Key: <broker_api_key>`.
   - We will add a Gateway refresh proxy endpoint to keep agents gateway-only.

### Endpoints Summary (current)
- Gateway
  - `POST /v1/request-connection`
  - `GET  /v1/check-connection/{connection_id}`
  - `GET  /v1/token/{connection_id}`
- Broker
  - `GET  /auth/callback` (provider redirect)
  - `POST /connections/{connection_id}/refresh` (temporary until gateway proxy)

### Security & Production Notes
- OIDC discovery and JWKS verification enabled; id_token is verified when present.
- Prefer tenant-specific issuer for Azure in production; `common` allowed for multi-tenant/personal.
- Keep Broker private; allowlist agent CIDRs and protect with API key if agents must call refresh directly.
- Agents should store only `connection_id`. Fetch tokens on-demand.

### Errors & Retries
- Gateway `GET /v1/token/{id}` returns upstream status codes. Handle 502/503 by retrying with backoff.
- If token expired/invalid, call refresh (Broker for now) then retry token fetch.

### Roadmap (Tech Debt)
- Gateway refresh proxy: `POST /v1/refresh/{connection_id}` and `auto_refresh` support on token fetch.
- Persistent discovery/JWKS cache and richer metrics.

