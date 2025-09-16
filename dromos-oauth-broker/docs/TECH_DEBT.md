## Technical Debt: OIDC Hardening for OAuth Broker

### Current Posture (updated)
- We use Authorization Code + PKCE, HMAC-signed `state`, HTTPS (assumed in prod), API key gate, IP allowlisting, return URL validation.
- Tokens are encrypted at rest (AES-GCM) and refreshed on-demand.
- Implemented OIDC basics:
  - `id_token` verification via JWKS (go-oidc): verifies signature, `iss`, `aud`, `exp`, and checks `iat` skew and `nonce`.
  - OIDC discovery with in-memory caching (prefers discovered `authorization_endpoint`/`token_endpoint`, falls back to static URLs if discovery unavailable).
  - `nonce` generated (bound via `state`) and verified when `openid` scope present.
- Still not implemented:
  - Persistent metadata/JWKS cache with ETag/Last-Modified and background refresh.
  - Provider data model fields (`issuer`, `enable_discovery`, optional overrides) and admin toggles.
  - Metrics for discovery/JWKS/verification outcomes; detailed logs for cache states.
  - `UserInfo` endpoint fetching/normalization.
  - Startup warm-up of discovery/JWKS and stronger retry/backoff.

### Risk Assessment
- Low risk if the broker remains internal and downstreams never rely on `id_token` claims (email/hd/sub/etc.).
- Elevated risk if any service parses or trusts `id_token` without verification:
  - Spoofed or substituted tokens (no signature verification, no `nonce`).
  - Audience/issuer mix-ups across providers.
  - Expired tokens used (no `exp`/`iat` checks).
  - Inconsistent profile data without `UserInfo` normalization.

### Triggers to Pay Down This Debt
- Any consumer begins to read `id_token` claims for identity, linking, or authorization.
- Broker is exposed beyond trusted network boundaries.
- Multi-provider usage where issuer/audience mix-up is possible.
- Compliance/security review requires OIDC-conformant validation.

### Acceptance Criteria
Completed:
- Verify `id_token` using provider JWKS (sig/iss/aud/exp + `iat` skew + `nonce`).
- Add and verify `nonce` for OIDC flows.
- Use OIDC discovery to resolve endpoints; prefer discovered endpoints at runtime.

Remaining:
- Cache JWKS/metadata persistently with rotation handling; conditional GETs.
- Provider model fields and flags: `issuer`, `enable_discovery`, `well_known_url`, `endpoint_overrides`, "prefer discovery" vs "force static".
- Metrics/logs for discovery/JWKS/verification outcomes and latencies.
- `UserInfo` integration (optional, when scopes allow).
- Tests: unit (edge cases: expired, wrong aud/iss/alg, nonce mismatch) and integration (e.g., Google/Azure).
- Feature flag to toggle OIDC-verified mode vs opaque-token mode.

### Implementation Outline (progress vs remaining)
1) Dependencies
   - [DONE] Add `go-oidc` and HTTP client timeouts.
   - [REMAINING] Persistent JWKS/metadata cache (DB + ETag/Last-Modified).

2) Data Model / Migration
   - [REMAINING] Add provider fields: `issuer`, `enable_discovery`, `well_known_url`, `endpoint_overrides`.

3) Consent Flow
   - [DONE] Generate/attach `nonce` (via signed state) when `openid` present.
   - [DONE] Use discovered `authorization_endpoint` when available; fallback to static.

4) Callback Flow
   - [DONE] Verify `id_token` (sig/iss/aud/exp/iat/nonce) via go-oidc before storing tokens.
   - [DONE] Prefer discovered `token_endpoint` when available; fallback to static.
   - [REMAINING] Audit/metrics for verification outcomes.

5) Configuration
   - [REMAINING] Feature flags to toggle discovery/verification modes; JWKS TTL/backoff settings.

6) Observability
   - [REMAINING] Metrics for discovery hits/misses, JWKS fetch, verification success/fail; structured logs for cache/refresh.

---

## Technical Debt: Gateway Refresh Proxy

### Current Posture
- Agents fetch tokens via gateway GET `/v1/token/{connection_id}`.
- Refresh is available at the broker: POST `/connections/{connection_id}/refresh` (requires broker API key + allowlist).
- Agents may need to call the broker directly to refresh.

### Goal
Provide a single front door for agents by adding a gateway endpoint to proxy token refresh to the broker, centralizing policy, metrics, and credentials.

### Acceptance Criteria
- Gateway exposes POST `/v1/refresh/{connection_id}` (or `GET /v1/token/{id}?auto_refresh=true`).
- Gateway calls broker refresh with its own API key and returns refreshed token JSON.
- Rate limits/backoff applied; metrics: `token_refresh_total`, `token_refresh_duration_seconds`.
- Backwards compatible; direct broker refresh continues to work.

### Rollout Plan
1) Implement gateway proxy endpoint and metrics.
2) Update agent docs to prefer the gateway endpoint.
3) Optionally restrict broker refresh to gateway CIDRs over time.

### Notes and Provider Specifics
- Google: issuer is `https://accounts.google.com`; do not request `offline_access` scope. Use `access_type=offline` and `prompt=consent` for refresh tokens.
- Non-Google providers (e.g., Microsoft/Okta) may use `offline_access` scope; keep provider-specific handling.

### Rollout Strategy
- Phase 1: Ship flag-disabled OIDC verification in parallel with existing flow; add metrics.
- Phase 2: Enable verification in staging; validate downstreams.
- Phase 3: Enable in production for providers where consumers rely on identity claims.


---

## Technical Debt: Service Mesh mTLS for Internal Traffic

### Current Posture
- App-layer guards in place (API key on sensitive routes, IP allowlist, return_url allowlist).
- Transport encryption between gateway ↔ broker is not enforced by mTLS yet.

### Goal
Enforce mutual TLS for all internal service-to-service traffic (e.g., gateway ↔ broker) via a service mesh (Istio/Linkerd/Consul) or Envoy sidecars to avoid app code changes and centralize cert management.

### Acceptance Criteria
- Workload certificates are issued/rotated automatically; peer authentication (mTLS) is enforced at STRICT mode for broker/gateway namespaces.
- Authorization policies restrict which workloads can call broker sensitive routes.
- Application pods do not manage TLS material; apps continue to speak HTTP/gRPC on localhost while the mesh secures on-the-wire traffic.
- Health checks and golden signals in place for mTLS (success/fail counts, certificate age, policy violations).

### Rollout Plan
1) Enable the mesh with PERMISSIVE mTLS in staging; verify traffic flows.
2) Switch to STRICT mTLS for broker/gateway namespaces; add allow policies for gateway → broker.
3) Add rate limits and per-route auth policies at the mesh/ingress layer; monitor and then promote to production.

### Interim Hardening (until mesh)
- Keep API key + IP allowlist enabled for sensitive routes.
- Enforce `sslmode=require` for Postgres connections.
- Terminate public TLS at ingress for `/auth/callback` with managed certificates.
- Rotate API keys and keep `ALLOWED_CIDRS` tight to gateway/ingress subnets.
- Apply basic 429/rate limits and WAF rules at ingress for sensitive broker endpoints.

---

## Technical Debt: Ingress TLS Termination and Rate Limiting

### Current Posture
- Broker serves HTTP internally; `/auth/callback` may be exposed publicly via VM IP.
- No standardized ingress configuration yet (TLS/WAF/rate limits).

### Goal
- Terminate TLS for public endpoints at a managed ingress (NGINX/Envoy/Azure Application Gateway) with automated certificates.
- Enforce 429/rate limits and WAF rules at the edge for `/auth/callback` and any temporarily exposed sensitive routes.

### Acceptance Criteria
- Public access only to `/auth/callback` via HTTPS (managed certs); all other broker routes blocked or restricted.
- Rate limiting configured (e.g., 10 req/min per IP for `/auth/callback`) and WAF protections enabled.
- Health checks configured; logs/metrics from ingress available.

### Rollout Plan
1) Staging: configure ingress with HTTPS, WAF, and rate limits; validate callback flow.
2) Production: point DNS, enable automatic cert renewal, monitor for throttling/blocks.
3) Remove any temporary public exposure of sensitive broker routes.


