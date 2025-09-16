n care of le## Technical Debt: OIDC Hardening for OAuth Broker

### Current Posture (as-built MVP)
- Tokens are treated as opaque; we do not use `id_token` claims for authorization decisions.
- We use Authorization Code + PKCE, HMAC-signed `state`, HTTPS (assumed in prod), API key gate, IP allowlisting, return URL validation.
- We store tokens encrypted at rest (AES-GCM) and support on-demand refresh via provider token endpoints.
- We do not currently:
  - Verify `id_token` signatures/claims (no JWKS-based verification).
  - Include/verify OIDC `nonce`.
  - Use OIDC discovery (`.well-known/openid-configuration`).
  - Call the OIDC `UserInfo` endpoint.

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

### Acceptance Criteria (when we harden OIDC)
- Verify `id_token` using provider JWKS:
  - Validate signature, `iss`, `aud` (matches our `client_id`), `exp`, `iat` (with small clock skew), and algorithm (reject `none`).
  - Cache JWKS keys with expiry and rotation handling.
- Add `nonce` to auth request and verify `nonce` in `id_token` at callback.
- Support OIDC discovery to resolve `issuer`, `authorization_endpoint`, `token_endpoint`, `jwks_uri`.
- Optional: Call `UserInfo` endpoint when `openid email profile` scopes are present; reconcile with `id_token` claims.
- Emit metrics/logs for verification outcomes (success/failure counters, latency histograms).
- Comprehensive tests:
  - Unit tests for verifier (valid/expired/wrong-aud/wrong-iss/wrong-alg/nonce-mismatch).
  - Integration smoke with at least one provider (e.g., Google) using discovery.
- Feature flag/toggle:
  - Backwards compatible: allow running in “opaque-token mode” (current behavior) vs “OIDC-verified mode”.

### Implementation Outline
1) Dependencies
   - Add `go-oidc` (v3) and an HTTP client with sane timeouts.
   - Implement a small JWKS cache (LRU with TTL) per issuer.

2) Data Model / Migration
   - Add `nonce` column to `connections` (nullable initially). Store per consent flow.

3) Consent Flow
   - Generate cryptographically random `nonce`; include in authorization request when `openid` present.

4) Callback Flow
   - Resolve discovery (or use configured issuer) → build `oidc.Verifier` with expected `client_id` and `issuer`.
   - Verify `id_token`, check `nonce`, then proceed with token storage/update as today.
   - Record audit events for verification failures.

5) Configuration
   - Flags/env: `OIDC_VERIFY_ID_TOKEN` (bool), `OIDC_USE_DISCOVERY` (bool), optional `OIDC_ISSUER_OVERRIDE` per provider.
   - Timeouts and JWKS cache TTL settings.

6) Observability
   - Metrics: `oidc_verifications_total{result=...}`, `oidc_verification_duration_seconds` histogram, JWKS fetch errors.
   - Structured logs with provider/issuer/connection_id (no secrets).

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


