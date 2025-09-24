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

### Frontend Integration (browser flow)
The browser initiates consent; server(s) hold secrets and call the gateway.

1) Agent/server asks gateway to create a connection
   - Your backend calls `POST /v1/request-connection` (see above) with a `return_url` hosted by your frontend (e.g., `https://app.example.com/oauth/return`).

2) Redirect the user
   - From your frontend, redirect the browser to the `authUrl` in the response.

3) Provider → Broker → Frontend
   - After consent, the provider redirects to the Broker callback; the Broker persists tokens and redirects the user back to your `return_url` with `status=success&connection_id=...`.

4) Frontend → Backend to consume connection
   - Your frontend extracts `connection_id` and sends it to your backend (e.g., via POST). Your backend stores only the `connection_id`.

5) Backend fetches tokens on-demand
   - Your backend calls Gateway `GET /v1/token/{connection_id}` to retrieve tokens when needed. Do not store tokens long-term; cache short-lived if required.

Frontend notes:
- Ensure the Broker `BASE_URL` is public and the exact callback URL is registered in provider consoles.
- Set Broker `ENFORCE_RETURN_URL=true` and list allowed domains in `ALLOWED_RETURN_DOMAINS`.
- You do not need to expose secrets to the browser. The browser only handles redirects.

### Security & Production Notes
- OIDC discovery and JWKS verification enabled; id_token is verified when present.
- Prefer tenant-specific issuer for Azure in production; `common` allowed for multi-tenant/personal.
- Keep Broker private; allowlist agent CIDRs and protect with API key if agents must call refresh directly.
- Agents should store only `connection_id`. Fetch tokens on-demand.

### Errors & Retries
- Gateway `GET /v1/token/{id}` returns upstream status codes. Handle 502/503 by retrying with backoff.
- If token expired/invalid, call refresh (Broker for now) then retry token fetch.

### Using the Go SDK (server-side)
Add the module and call the Gateway via the SDK:

```go
import (
  "context"
  oauthsdk "github.com/dromos-labs/oauth-framework/oauth-sdk"
)

client := oauthsdk.New(
  "https://<gateway-base-url>",
  oauthsdk.WithRetry(oauthsdk.RetryPolicy{Retries: 3}),
)

rc, _ := client.RequestConnection(context.Background(), oauthsdk.RequestConnectionInput{
  UserID:       "workspace-123",
  ProviderName: "Google",
  Scopes:       []string{"openid","email","profile"},
  ReturnURL:    "https://app.example.com/oauth/return",
})
// Redirect user to rc.AuthURL

// Later, fetch token:
tok, _ := client.GetToken(context.Background(), rc.ConnectionID)
```

### Roadmap (Tech Debt)
- Gateway refresh proxy: `POST /v1/refresh/{connection_id}` and `auto_refresh` support on token fetch.
- Persistent discovery/JWKS cache and richer metrics.

