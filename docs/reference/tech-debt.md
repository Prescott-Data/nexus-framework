# Technical Debt & Roadmap

This document consolidates technical debt and future roadmap items across the Dromos OAuth Framework.

## 1. Gateway Refresh Proxy (High Priority)

**Status:** Planned
**Impact:** Agents currently have to call the internal Broker directly to force a refresh, breaking the abstraction layer.

**Goal:**
Add a `POST /v1/refresh/{connection_id}` endpoint to the **Gateway**.
- This endpoint should proxy the request to the Broker's refresh endpoint (`POST /connections/{id}/refresh`).
- It should use the Gateway's internal API key to authenticate with the Broker.
- It eliminates the need for agents to know about the Broker's existence or possess its API key.

## 2. OIDC Hardening (Broker)

**Status:** Partially Implemented
**Impact:** `id_token` verification is basic. Use with caution for identity assertions.

**Completed:**
- `id_token` verification via JWKS (signature, `iss`, `aud`, `exp`, `nonce`).
- OIDC discovery (in-memory caching).
- `nonce` generation and validation.

**Remaining:**
- **Persistent Caching:** Store JWKS/metadata in DB/Redis with ETag/Last-Modified support to survive restarts and reduce upstream calls.
- **Provider Configuration:** Add explicit fields to `provider_profiles` for `issuer`, `enable_discovery`, `well_known_url`. Currently, discovery is "best effort".
- **Observability:** Add specific metrics for discovery hits/misses and verification failures.
- **UserInfo:** Implement `UserInfo` endpoint fetching and normalization.

## 3. Service Mesh mTLS (Infrastructure)

**Status:** Planned
**Impact:** Internal communication between Gateway and Broker relies on API Keys and IP Allowlisting.

**Goal:**
- Implement mTLS (e.g., via Linkerd, Istio, or Azure Container Apps mTLS) for the `Gateway -> Broker` path.
- Once mTLS is in place, we can potentially relax IP allowlisting for internal traffic, though API Keys (identity) should remain for audit trails.

## 4. Bridge Design Decisions

### `RequireTransportSecurity` defaults to `false`
- The `bridge` library defaults to insecure transport for gRPC to facilitate local development.
- **Trade-off:** Users must explicitly enable TLS credentials when deploying to production. This is documented in the Bridge README.
