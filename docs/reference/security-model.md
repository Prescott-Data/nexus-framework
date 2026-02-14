# Broker Security Guide

This document describes the security measures implemented in the Nexus OAuth Broker and how to operate them in development and production.

## App-layer Guardrails

- **API Key on sensitive routes**
  - Header: `X-API-Key`
  - Env:
    - `REQUIRE_API_KEY=true|false` (dev default: false)
    - `API_KEY` (single) or `API_KEYS` (comma-separated)
  - Applied routes:
    - `POST /providers`, `GET /providers`, `GET /providers/by-name/*`
    - `POST /auth/consent-spec`
    - `GET /connections/{id}/token`, `POST /connections/{id}/refresh`
  - Not applied: `GET /auth/callback`, `GET /health`, `GET /metrics`

- **IP Allowlist**
  - Env:
    - `REQUIRE_ALLOWLIST=true|false` (dev default: false)
    - `ALLOWED_CIDRS` (e.g., `127.0.0.1/32,::1/128,10.0.0.0/24`)
  - Applied to the same sensitive routes as API key. `GET /auth/callback` remains public for provider redirects.

- **Return URL Allowlist (open redirect defense)**
  - Env:
    - `ENFORCE_RETURN_URL=true|false` (dev default: false)
    - `ALLOWED_RETURN_DOMAINS` (comma-separated hosts; optional `*.example.com`)
  - Validated in both `POST /auth/consent-spec` and `GET /auth/callback`.

## OAuth/OIDC Hardening

- HMAC-signed `state` with TTL.
- PKCE S256 for all consents.
- Provider nuances:
  - Google: remove `offline_access`; add `access_type=offline` and `prompt=consent`.
  - Azure v2: gateway logs a guidance hint if only OIDC/base scopes are provided (suggest `User.Read`). Broker does not auto-mutate scopes.
- OIDC id_token verification (JWKS/iss/aud/nonce) is tracked in `docs/TECH_DEBT.md` and will be introduced behind a feature flag.

## Transport & Data at Rest

- **Database TLS**
  - Env-controlled enforcement in DSN:
    - `ENFORCE_DB_SSL=true` to enforce TLS
    - `DB_SSLMODE=require|verify-ca|verify-full` (default: `require`)
    - `DB_SSLROOTCERT=/path/to/ca.pem` for `verify-ca/verify-full`
  - Recommendation: `verify-full` in production.
- **Token encryption**: AES-GCM with 32-byte key (`ENCRYPTION_KEY`), stored ciphertext only. Keys are not logged.

## Observability & Ops

- Metrics (Prometheus)
  - `oauth_consents_created_total`, `oauth_consents_with_openid_total`
  - `oauth_token_exchanges_total{status}` and `oauth_exchange_duration_seconds`
  - `oauth_id_tokens_returned_total`
  - `oauth_token_get_total{provider,has_id_token}`
- Structured logs with redaction of sensitive fields; include `request_id` when available.
- GC/maintenance: pending connections TTL enforced in DB; consider periodic cleanup of expired/stale rows.

## Ingress and Rate Limiting (Edge)

- TLS termination and rate limiting at ingress (NGINX/Envoy/Azure Application Gateway) are recommended and tracked in `docs/TECH_DEBT.md`.
  - Publicly expose only `GET /auth/callback`.
  - Apply `429` limits and WAF for public endpoints.

## Development vs Production

- **Development defaults** (easy local testing):
  - `REQUIRE_API_KEY=false`, `REQUIRE_ALLOWLIST=false`, `ENFORCE_RETURN_URL=false`
  - `ALLOWED_RETURN_DOMAINS=localhost,127.0.0.1,::1`
- **Production guidance**:
  - Enable all three guards and keep allowlist tight to gateway/ingress subnets.
  - Use `sslmode=verify-full` to Postgres and managed TLS at ingress.
  - Rotate API keys regularly; monitor metrics and logs.

## Future Work (Tech Debt)

See `docs/TECH_DEBT.md` for:
- Service mesh mTLS for internal S2S (gateway ↔ broker)
- OIDC id_token verification and discovery
- Ingress TLS/WAF/rate limiting rollout plans
