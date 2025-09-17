## Dromos OAuth Broker

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
source .env && go run ./cmd/oauth-broker
```
Health check:
```bash
curl -s http://localhost:8080/health
```

---

## Register Providers

Endpoint requires payload under `profile`.

### Google
```bash
jq -n '{
  profile: {
    name: "google",
    auth_url: "https://accounts.google.com/o/oauth2/v2/auth",
    token_url: "https://oauth2.googleapis.com/token",
    client_id: "<client-id>",
    client_secret: "<client-secret>",
    scopes: ["openid","email"]
  }
}' | curl -s -X POST http://localhost:8080/providers -H "Content-Type: application/json" -d @- | jq .
```
Notes: We do not request `offline_access` scope for Google. The broker adds `access_type=offline` and `prompt=consent` URL params to obtain a refresh token.

### Microsoft Entra ID (Azure AD) – multitenant + personal
```bash
jq -n '{
  profile: {
    name: "azure-ad-common",
    auth_url: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
    token_url: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
    client_id: "<application-client-id-guid>",
    client_secret: "<client-secret-value>",
    scopes: ["openid","email","profile","offline_access","User.Read"]
  }
}' | curl -s -X POST http://localhost:8080/providers -H "Content-Type: application/json" -d @- | jq .
```
More examples (Okta/Auth0) in `docs/PROVIDERS.md`.

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
    "return_url":"http://localhost:8080/health"
  }' | jq .
```
Open `.authUrl` in a browser and complete consent. You’ll be redirected to `return_url` with `connection_id`.

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
go build -o oauth-broker ./cmd/oauth-broker
```

---

## See also
- `docs/PROVIDERS.md` – registry and templates for supported providers
- `docs/TECH_DEBT.md` – OIDC hardening plan and acceptance criteria

Touching to test 

