# Dromos OAuth Framework

This repository contains two services that together provide a secure, provider‑agnostic OAuth 2.0/OIDC integration layer:

- `dromos-oauth-broker` (backend): Handles provider discovery, consent spec (PKCE + signed state), callback/code exchange, token encryption/storage, token retrieval and refresh, and Prometheus metrics.
- `dromos-oauth-gateway` (front door): Exposes a stable gRPC/HTTP API for agents and services. It initiates connections via the broker, lets clients poll connection status, and fetches tokens on demand. It does not store tokens.

Both services support OIDC discovery and JWKS-based `id_token` verification (issuer/audience/exp/iat/nonce), Prometheus metrics, structured logs, and production hardening controls (API key, allowlist, return URL enforcement).

---

## Architecture Overview

1) Agent (server) requests a connection from the Gateway → Gateway calls the Broker to build consent (PKCE + signed state) and returns `authUrl` + `connection_id`.
2) User completes consent at the provider → Provider redirects to Broker callback → Broker exchanges code for tokens, encrypts/stores, marks connection `active`, and redirects user to the agent’s `return_url` with `connection_id`.
3) Agent fetches tokens on demand from the Gateway using `connection_id`. If expired, the broker supports refresh; a Gateway refresh proxy will be added.

See `INTEGRATIONS.md` for step-by-step flows (agent and frontend).

---

## Key Endpoints

Gateway (HTTP):
- POST `/v1/request-connection`
- GET  `/v1/check-connection/{connection_id}`
- GET  `/v1/token/{connection_id}`
- GET  `/metrics` (Prometheus)

Broker (HTTP):
- POST `/auth/consent-spec`
- GET  `/auth/callback` (provider redirect)
- GET  `/connections/{connection_id}/token`
- POST `/connections/{connection_id}/refresh`
- GET  `/metrics` (Prometheus)

---

## Configuration (high-level)

Shared:
- `STATE_KEY` (base64‑32B): HMAC signing for `state` and nonce binding. Must match between Gateway and Broker.

Broker (required in prod):
- `DATABASE_URL` (PostgreSQL)
- `BASE_URL` (public URL, e.g., `https://<broker-domain>`)
- `REDIRECT_PATH` (default `/auth/callback`)
- `ENCRYPTION_KEY` (base64‑32B) – AES‑GCM for tokens
- `API_KEY`, `REQUIRE_API_KEY`, `REQUIRE_ALLOWLIST`, `ALLOWED_CIDRS`, `ENFORCE_RETURN_URL`, `ALLOWED_RETURN_DOMAINS`

Gateway:
- `PORT`
- `BROKER_BASE_URL` (Broker URL – public if outside ACA, internal if on the same ACA env)
- `STATE_KEY` (same as Broker)

See service READMEs for full environment matrices.

---

## Agent Integration (summary)

- Store only `connection_id` in your system.
- Flow:
  1) `POST /v1/request-connection` → returns `authUrl` + `connection_id`; redirect user to `authUrl`.
  2) After callback, poll `GET /v1/check-connection/{connection_id}` until `active`.
  3) Fetch tokens on demand: `GET /v1/token/{connection_id}` and use `access_token` for provider APIs.
- Refresh: call Broker `POST /connections/{connection_id}/refresh` with `X-API-Key` (temporary). A Gateway refresh proxy will be added.

Full details and frontend notes: `INTEGRATIONS.md`.

---

## Build & Run (local quickstart)

Broker:
```bash
cd dromos-oauth-broker
source .env && go run ./cmd/oauth-broker
```

Gateway (REST server):
```bash
cd dromos-oauth-gateway
export BROKER_BASE_URL="http://localhost:8080"
export STATE_KEY="$(openssl rand -base64 32)" # use broker's key in real env
go run ./cmd/oha
```

---

## Deployment (Azure Container Apps)

- Build & push images via Bitbucket Pipelines (conditional per service).
- Ensure the Container Apps can pull from ACR (Managed Identity `AcrPull` or registry creds).
- Broker must be externally reachable for provider callbacks; set `BASE_URL=https://<broker-public-url>` and register the exact redirect URI in provider consoles.
- Gateway should point `BROKER_BASE_URL` to the Broker (internal or public depending on network).

---

## Observability

Prometheus targets (example):
```yaml
- job_name: 'oauth-broker'
  scheme: https
  metrics_path: /metrics
  static_configs:
    - targets: ['<broker-domain>']

- job_name: 'oauth-gateway'
  scheme: https
  metrics_path: /metrics
  static_configs:
    - targets: ['<gateway-domain>']
```

Key metrics:
- Broker: `oauth_consents_created_total`, `oauth_token_exchanges_total{status}`, `oauth_token_get_total{provider,has_id_token}`
- OIDC: `oidc_discovery_total`, `oidc_discovery_duration_seconds`, `oidc_verifications_total{result}`, `oidc_verification_duration_seconds`

---

## Service Docs

- Broker details: `dromos-oauth-broker/README.md`
- Gateway details: `dromos-oauth-gateway/README.md`
- Integration guide: `INTEGRATIONS.md`
- OpenAPI spec: `openapi.yaml` (v1 frozen endpoints)
- Provider registry & examples: `dromos-oauth-broker/docs/PROVIDERS.md`
- Tech debt & roadmap: `dromos-oauth-broker/docs/TECH_DEBT.md`
