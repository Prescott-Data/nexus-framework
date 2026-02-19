# Nexus Framework Roadmap

This document outlines the strategic direction and planned improvements for the Nexus Framework. We welcome community contributions to help us achieve these goals.

## 1. OIDC Hardening & Security

**Status:** Partially Implemented
**Goal:** Enhance the robustness of Identity assertions and caching mechanisms.

**Completed:**
- `id_token` verification via JWKS (signature, `iss`, `aud`, `exp`, `nonce`).
- OIDC discovery (in-memory caching).
- `nonce` generation and validation.
- **Provider Configuration:** Explicit fields added to `provider_profiles` (`issuer`, `enable_discovery`, `user_info_endpoint`).
- **Observability:** Metrics added for discovery hits/misses (`oidc_discovery_total`) and verification results (`oidc_verifications_total`).

**Remaining:**
- **Persistent Caching:** Store JWKS/metadata in DB/Redis with ETag/Last-Modified support to survive restarts and reduce upstream calls. Currently uses in-memory/request-scoped discovery.
- **UserInfo:** Implement `UserInfo` endpoint fetching and normalization. Field exists in DB but logic is missing.

## 2. Infrastructure Security (Transport Encryption)

**Status:** Planned
**Goal:** Encrypt internal traffic between `Gateway -> Broker` to satisfy "Encryption in Transit" requirements.

**Strategy:**
- **Preferred:** Use platform-native mTLS/TLS if available (e.g., AWS App Mesh, Azure Container Apps, Google Cloud Run).
- **Alternative:** Configure standard TLS 1.3 in the Go `http.Server` (Broker) and `http.Client` (Gateway) using self-signed certs or a private CA.
- **Note:** A full Service Mesh (Istio/Linkerd) is currently considered overkill for this architecture.

## 3. Resilient Connection Handling

**Status:** Partially Implemented
**Goal:** Explicitly handle unrecoverable refresh errors to prevent endless retry loops.

**Implementation:**
- **Broker Logic:** Completed. `RefreshToken` logic detects 4xx errors (e.g., `invalid_grant`) and transitions connection status to `attention`. Returns `409 Conflict` with `error: attention_required`.
- **Bridge Logic:** Pending. Bridge needs update to recognize `attention_required` error (409) and stop retrying.

## 4. Interactive Authentication Flows

**Status:** Partially Implemented
**Goal:** Support human-in-the-loop flows for when provider refreshes fail due to MFA or CAPTCHA requirements.

**Implementation Details:**
1.  **Broker Logic:** Completed. Transitions connection status to `attention` on specific errors.
2.  **Bridge Logic:** Pending. Needs to stop retrying and emit `ErrInteractionRequired`.
3.  **Frontend/Notification:** Planned. Enable system to trigger user alerts.

## 5. Architecture: The Sidecar Model

**Status:** Proposed / Long-Term
**Goal:** Achieve zero-knowledge agent architecture by moving signing logic out of the application process.

**Context:**
Currently, the `bridge` runs as a library within the Agent's process. If compromised, usage secrets in RAM could be exposed.

**The Solution:**
Develop `nexus-sidecar`, a standalone proxy service deployed alongside the Agent.
1.  **Traffic Flow:** Agent sends unauthenticated HTTP/gRPC requests to `localhost:sidecar_port`.
2.  **Interception:** The Sidecar intercepts the request.
3.  **Signing:** The Sidecar fetches the credentials from the Gateway/Broker and signs the request.
4.  **Forwarding:** The Sidecar forwards the authenticated request to the upstream Provider.

**Benefits:**
- **Zero-Knowledge Agent:** Even with RCE, an attacker finds no keys in the Agent's memory.
- **Polyglot Support:** Any language can use Nexus via simple HTTP requests to localhost.

## 6. Production Hardening: CORS

**Status:** Open
**Priority:** Low (Staging/Dev only)
**Date Logged:** 2026-01-27

**Description:**
The AllowedOrigins for CORS in `nexus-gateway` (both REST and gRPC) are currently hardcoded to `["https://*", "http://*"]` to unblock staging.

**Required Action:**
Refactor the CORS configuration to read the allowed origins from an environment variable (e.g., `CORS_ALLOWED_ORIGINS`). This will allow strict enforcement of the frontend domain in production environments without code modification.
