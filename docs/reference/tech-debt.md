# Technical Debt & Roadmap

This document consolidates technical debt and future roadmap items across the Dromos OAuth Framework.

## 1. Gateway Refresh Proxy (High Priority)

**Status:** Completed
**Impact:** Agents currently have to call the internal Broker directly to force a refresh, breaking the abstraction layer.

**Goal:**
Add a `POST /v1/refresh/{connection_id}` endpoint to the **Gateway**.
- This endpoint should proxy the request to the Broker's refresh endpoint (`POST /connections/{id}/refresh`).
- It should use the Gateway's internal API key to authenticate with the Broker.
- It eliminates the need for agents to know about the Broker's existence or possess its API key.

**Implementation Details:**
- Added `POST /v1/refresh/{connection_id}` to Gateway HTTP API.
- Added `RefreshConnection` RPC to Gateway gRPC API.
- Gateway proxies request to Broker using internal client.

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

## 5. Formal Connection States (Database Migration)

**Status:** Planned
**Impact:** Currently, connection states are loosely defined (`active`, `pending`, `failed`). The Nexus Protocol now defines formal states (`REVOKED`, `EXPIRED`) which are not yet enforced in the DB.

**Goal:**
- Update `connections` table to use a formal Enum or Constraint for states.
- Update Broker logic to explicitly transition to `EXPIRED` (for unrecoverable refreshes) and `REVOKED` (for admin actions).
- Ensure Bridge handles `401` by transitioning local state effectively.

## 6. Agent Isolation & The Sidecar Model (Security Roadmap)

**Status:** Proposed / Long-Term
**Context:** Currently, the `bridge` runs as a library within the Agent's process.
- **Risk:** If the Agent process is compromised (RCE), the attacker can dump process memory. This reveals the **Usage Secrets** (Access Tokens, API Keys, Signing Secrets) currently held in the Bridge's RAM.
- **Mitigation (Current):**
    - Dromos ensures these are *only* Usage Secrets. The Master Secrets (Refresh Tokens) remain encrypted in the Broker.
    - Usage Secrets are short-lived (OAuth) or can be centrally rotated/revoked (Static Keys) without redeploying the Agent.
    - This offers a significantly smaller blast radius than finding a `.env` file with long-lived root keys.

**The "Sidecar" Solution:**
To achieve perfect isolation where the Agent *never* holds a secret in RAM, we must move the signing logic out of the process.

**Goal:**
Develop `dromos-sidecar`, a standalone proxy service deployed alongside the Agent (e.g., in the same Kubernetes Pod).
1.  **Traffic Flow:** Agent sends unauthenticated HTTP/gRPC requests to `localhost:sidecar_port`.
2.  **Interception:** The Sidecar intercepts the request.
3.  **Signing:** The Sidecar fetches the credentials from the Gateway/Broker and signs the request (injects headers, calculates SigV4).
4.  **Forwarding:** The Sidecar forwards the authenticated request to the upstream Provider (AWS, Google, etc.).

**Benefits:**
- **Zero-Knowledge Agent:** Even with RCE, an attacker finds no keys in the Agent's memory.
- **Polyglot Support:** Any language (Python, Rust, Node) can use Dromos just by sending HTTP requests to localhost; no language-specific SDK/Bridge required.

**Trade-offs:**
- Increased infrastructure complexity (managing sidecars).
- Added network latency (hop to localhost).
- Higher resource consumption (CPU/RAM for the sidecar process).