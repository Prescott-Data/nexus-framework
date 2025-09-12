## Technical Debt: OIDC Hardening for OAuth Broker

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


