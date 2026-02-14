## Nexus Broker

Minimal, internal OAuth 2.0/OIDC broker written in Go. It initiates consent (PKCE + state), exchanges codes for tokens, and stores tokens encrypted in PostgreSQL. Tokens are treated as opaque; we do not rely on id_token claims by default.

### Key Capabilities
- Provider registry (DB-backed)
- Consent-spec builder (PKCE + HMAC state)
- OAuth callback and code exchange
- AES-GCM token vault (encrypted at rest)
- Token retrieval and on-demand refresh
- Security gates (API key, IP allowlist, return URL validation)
- Ready for service-mesh mTLS (tracked in `docs/TECH_DEBT.md`)
- Prometheus metrics and structured logging
- **Integration Metadata:** Exposes API base URLs and endpoints for frontend discovery.

---

## Setup

### 1) Start PostgreSQL (dev)
```bash
docker-compose up -d postgres
```

Enable pgcrypto once:
```bash
psql "postgres://oauth_user:oauth_password@localhost/oauth_broker?sslmode=disable" \
  -c "CREATE EXTENSION IF NOT EXISTS pgcrypto;"
```

### 2) Configure environment
Create `.env` (see `.env.example`). Minimal:
```bash
DATABASE_URL=postgres://oauth_user:oauth_password@localhost/oauth_broker?sslmode=disable
BASE_URL=http://localhost:8080
ENCRYPTION_KEY=<32-byte base64, stable>
STATE_KEY=<32-byte base64, stable>
REDIRECT_PATH=/auth/callback
API_KEY=dev-api-key-12345
ALLOWED_CIDRS=127.0.0.1/32,::1/128
ALLOWED_RETURN_DOMAINS=localhost,127.0.0.1,::1
PORT=8080
```
Generate keys (dev):
```bash
openssl rand -base64 32  # use output for ENCRYPTION_KEY
openssl rand -base64 32  # use output for STATE_KEY
```

Keep ENCRYPTION_KEY and STATE_KEY constant; changing them breaks decrypting stored tokens.

### 3) Run the broker
```bash
source .env && go run ./cmd/nexus-broker
```
Health check:
```bash
curl -s http://localhost:8080/health
```

---

## Register Providers

Endpoint requires payload under `profile`.

### Partial Updates (PATCH)
Use `PATCH /providers/{id}` to update specific fields without overwriting the entire profile (e.g. updating scopes only).
```bash
curl -X PATCH http://localhost:8080/providers/<id> \
  -H "Content-Type: application/json" \
  -d '{"scopes": ["new", "scope"]}'
```

### New Fields (v2)
- `auth_header`: Set to `"client_secret_basic"` for providers requiring Basic Auth (Twitter, GitHub). Default is `"client_secret_post"` (Body).
- `api_base_url`: Root URL for the provider's API (e.g., `https://api.github.com`). Used by frontend.
- `user_info_endpoint`: Path to fetch user profile (e.g., `/user`). Used by frontend.

### Google
```bash
jq -n '{
  profile: {
    name: "google",
    auth_type: "oauth2",
    auth_url: "https://accounts.google.com/o/oauth2/v2/auth",
    token_url: "https://oauth2.googleapis.com/token",
    client_id: "<client-id>",
    client_secret: "<client-secret>",
    scopes: ["openid","email","profile"],
    api_base_url: "https://www.googleapis.com",
    user_info_endpoint: "/oauth2/v3/userinfo"
  }
}' | curl -s -X POST http://localhost:8080/providers -H "Content-Type: application/json" -d @- | jq .
```

### Twitter (Requires Basic Auth)
```bash
jq -n '{
  profile: {
    name: "twitter",
    auth_type: "oauth2",
    auth_url: "https://twitter.com/i/oauth2/authorize",
    token_url: "https://api.twitter.com/2/oauth2/token",
    client_id: "<client-id>",
    client_secret: "<client-secret>",
    scopes: ["tweet.read","users.read"],
    auth_header: "client_secret_basic",
    api_base_url: "https://api.twitter.com/2",
    user_info_endpoint: "/users/me"
  }
}' | curl -s -X POST http://localhost:8080/providers -H "Content-Type: application/json" -d @- | jq .
```

### Microsoft Graph (Common)
```bash
jq -n '{
  profile: {
    name: "microsoft-graph",
    auth_type: "oauth2",
    auth_url: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
    token_url: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
    client_id: "<application-client-id-guid>",
    client_secret: "<client-secret-value>",
    scopes: ["openid","email","profile","offline_access","User.Read"],
    api_base_url: "https://graph.microsoft.com/v1.0",
    user_info_endpoint: "/me"
  }
}' | curl -s -X POST http://localhost:8080/providers -H "Content-Type: application/json" -d @- | jq .
```
**Note:** Ensure your Azure App Registration has a **Web** platform configured with the correct Redirect URI.

### OIDC & Self-Discovery
If you request the `openid` scope, the Broker attempts **OIDC Discovery**:
1. It uses the configured `auth_url` or `token_url` as a hint to find `/.well-known/openid-configuration`.
2. If found, it **dynamically uses** the endpoints (Authorization, Token, UserInfo) declared in the metadata, ignoring your manually configured values if they differ.
3. This simplifies maintenance for providers like Google, Okta, and Microsoft—you just need a valid "base" URL.

---

## Provider Metadata (Frontend Integration)

Retrieve a grouped list of all configured providers and their API metadata. Useful for building dynamic "Connect" UIs.

```bash
curl -H "X-API-Key: $API_KEY" http://localhost:8080/providers/metadata
```

**Response:**
```json
{
  "oauth2": {
    "google": {
      "api_base_url": "https://www.googleapis.com",
      "user_info_endpoint": "/oauth2/v3/userinfo",
      "scopes": ["openid", "email", "profile"]
    },
    "twitter": {
      "api_base_url": "https://api.twitter.com/2",
      "user_info_endpoint": "/users/me",
      "scopes": ["tweet.read", "users.read"]
    }
  },
  "api_key": { ... }
}
```

---

## Perform a Consent

Request the consent spec to get an authorization URL:
```bash
curl -s -X POST http://localhost:8080/auth/consent-spec \
  -H "Content-Type: application/json" \
  -d '{
    "workspace_id":"ws-123",
    "provider_id":"<provider_id>",
    "scopes":["openid","email"],
    "return_url":"http://localhost:3000/my-app-callback"
  }' | jq .
```
Open `.authUrl` in a browser and complete consent. You’ll be redirected to your `return_url` with `connection_id`, `status`, and `provider` as query parameters.

---

## Retrieve and Refresh Tokens

Token retrieval (gated by API key + IP allowlist):
```bash
curl -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/connections/<connection_id>/token"
```

On-demand refresh:
```bash
curl -X POST -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/connections/<connection_id>/refresh"
```

---

## Metrics and Logging

Prometheus at `/metrics`:
- `oauth_consents_created_total`
- `oauth_consents_with_openid_total`
- `oauth_token_exchanges_total{status=success|error}`
- `oauth_exchange_duration_seconds`
- `oauth_id_tokens_returned_total`
- `oauth_token_get_total{provider,has_id_token}`

Access logs are structured; audit events are recorded in `audit_events`.

---

## Security
- PKCE and HMAC-signed state on every consent
- AES-GCM token encryption; keys never logged
- API key required for sensitive endpoints (use `X-API-Key`)
- IP allowlisting via `ALLOWED_CIDRS`
- Return URL domain validation via `ALLOWED_RETURN_DOMAINS`
- Always use HTTPS in production (set `BASE_URL=https://...`)
- mTLS via service mesh planned; see `docs/TECH_DEBT.md`

See `docs/SECURITY.md` for detailed guardrails and operations.

OIDC hardening (id_token verification via JWKS, nonce, discovery) is deferred. See `docs/TECH_DEBT.md`.

---

## Troubleshooting
- invalid_scope (Google) for `offline_access`: remove; broker already adds Google-specific refresh params.
- redirect_uri_mismatch: ensure provider console matches `BASE_URL + REDIRECT_PATH` exactly.
- Failed to decrypt token: likely `ENCRYPTION_KEY` changed. Keep it stable and re-consent.
- Provider not found / TEXT[] scan errors: ensure we use `pq.Array` for scopes (handled in code) and correct UUID types.
- 404 on Google authorize: use `https://accounts.google.com/o/oauth2/v2/auth` (not legacy URLs).
- pgcrypto missing: `CREATE EXTENSION IF NOT EXISTS pgcrypto;` on your DB.
- **Token exchange failed (Twitter/GitHub):** Ensure you set `"auth_header": "client_secret_basic"` in the provider profile.
- **Token exchange failed (Microsoft):** Ensure your Azure App Registration has a **Web** platform (not SPA/Public).

---

## Production Notes
- Use managed Postgres (e.g., Azure Flexible Server). Set `sslmode=require`.
- Restrict DB network (VNet/private DNS or firewall IPs). Restrict broker sensitive routes by IP.
- Keep keys constant; rotate API key; monitor `/metrics`.
- Document each provider in `docs/PROVIDERS.md` when added.

---

## Development
Run tests:
```bash
go test ./...
```
Build:
```bash
go build -o nexus-broker ./cmd/nexus-broker
```

---

## See also
- `docs/PROVIDERS.md` – registry and templates for supported providers
- `docs/TECH_DEBT.md` – OIDC hardening plan and acceptance criteria

Touching to test pipeline

<!-- trigger build Mon Jan 26 11:07:00 EAT 2026 -->
<!-- trigger build Tue Jan 27 12:39:45 EAT 2026 env update -->

Triggering broker build

