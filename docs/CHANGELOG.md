---
icon: material/history
hide:
  - toc
---

# Changelog

All notable changes to Nexus are documented here. This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

<div class="changelog-release" markdown>

## Unreleased <span class="changelog-date">2026-05-14</span>

<div class="changelog-meta" markdown>
<div class="changelog-contributors">
<a href="https://github.com/sangalo20" title="Sangalo Mwenyinyo"><img src="https://github.com/sangalo20.png?size=32" alt="sangalo20"></a>
<a href="https://github.com/ekizito96" title="Muyukani Ephraim Kizito"><img src="https://github.com/ekizito96.png?size=32" alt="ekizito96"></a>
</div>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/commits/main" target="_blank" rel="noopener noreferrer">View commits on GitHub →</a>
</div>

**Added**

- **Python SDK** (`nexus-sdk-python`): full-feature-parity Python client — `get_token_by_connection_id`, `resolve_token`, `request_connection`, `check_connection`. Zero external dependencies.
- **TypeScript SDK** (`@dromos/nexus-sdk`): evolved from `nexus-mcp-adapter`. MCP token resolution, in-memory caching, authenticated transport, and `resolveToken` for stateless MCP clients. Built to `dist/`, ESM imports hardened.
- **Go SDK MCP integration**: `ResolveToken` endpoint for stateless MCP clients — workspace and provider-scoped token resolution with TTL caching.
- **Multi-strategy credential support** across all three SDKs: handles `oauth2`, `api_key`, `basic_auth`, `aws_sigv4`, `query_param`, and `hmac_payload` strategies without caller-side branching.
- **Automated release workflow**: GitHub Actions CI/CD pipeline, bumped `VERSION` to `0.2.3`.
- **Agent auth proposal** (`AGENT_AUTH_PROPOSAL.md`): full design document for agent identity, OBO sessions, scoped session TTLs, and custom scope enforcement.
- **SDK documentation**: comprehensive reference pages for Go, TypeScript, and Python SDKs including install, method signatures, MCP integration examples, and error handling.

**Fixed**

- TypeScript SDK: `Bearer` token type normalized to RFC 6750 capitalization (was `bearer`).
- TypeScript SDK: package entry pointed at compiled `dist/`, not `.ts` source.
- Gateway: `resolve` route wired; ESM import errors resolved in adapter.
- Adapter/Gateway: token TTL hardened, stdio safety improved, error handling tightened.
- MCP adapter smoke test added against live Gateway.

</div>

---

<div class="changelog-release" markdown>

## 0.2.0 <span class="changelog-date">2026-05-05</span>

<div class="changelog-meta" markdown>
<div class="changelog-contributors">
<a href="https://github.com/sangalo20" title="Sangalo Mwenyinyo"><img src="https://github.com/sangalo20.png?size=32" alt="sangalo20"></a>
<a href="https://github.com/Abdullahi254" title="Abdullahi Mohamud"><img src="https://github.com/Abdullahi254.png?size=32" alt="Abdullahi254"></a>
</div>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.2.0" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

**Added**

- **Security-as-Code CLI** (`nexus-cli`): Terraform-style `plan → confirm → apply` workflow for declarative provider management via YAML manifest. PATCH-based reconciliation (no accidental overwrites), concurrent profile fetching with bounded worker pool, field-level diff output with secret masking, fail-fast on unresolved env vars, non-zero exit on partial apply failure.
- **Audit subsystem** (`audit.Service`): structured event logging to `audit_events` table with IP validation, User-Agent capture, and `audit.Logger` interface for test mocking. Events: `provider.created`, `provider.updated`, `provider.deleted`, `connection.created`, `token.retrieved`, `token.refresh_fatal`.
- **`GET /audit` endpoint**: queryable audit log with `event_type`, `resource_id`, `since`, `until`, `limit`, and `offset` filters.
- **Credential redaction**: `PATCH` audit payloads redact `client_secret` and `client_id` before writing to the audit log.
- **Provider `category` field**: `category` added to provider profiles with migration. Gateway `MetadataResponse` patched to include `category` in the OpenAPI-generated response.
- **`capture-schema` and `capture-credential` endpoints**: Gateway proxies for static credential capture flow, enabling API key and basic auth connections without OAuth redirects.

**Fixed**

- Gateway: manually patched `MetadataResponse` to include `category` field, avoiding `oapi-codegen` version mismatch.
- Documentation: all examples standardized to `localhost:8090` — internal Azure URLs removed.
- OpenAPI: `description` and `category` added to `MetadataResponse` and `ProviderProfile` schemas; gateway broker client regenerated.

</div>

---

<div class="changelog-release" markdown>

## 0.1.5 <span class="changelog-date">2026-04-13</span>

<div class="changelog-meta" markdown>
<div class="changelog-contributors">
<a href="https://github.com/sangalo20" title="Sangalo Mwenyinyo"><img src="https://github.com/sangalo20.png?size=32" alt="sangalo20"></a>
<a href="https://github.com/ashioyajotham" title="Victor Ashioya"><img src="https://github.com/ashioyajotham.png?size=32" alt="ashioyajotham"></a>
</div>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.1.5" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

**Changed**

- Bridge: replaced `goto Retry` with `for`-loop in `MaintainGRPCConnection` — cleaner control flow, no goto jumps. ([@ashioyajotham](https://github.com/ashioyajotham))
- Broker: replaced streaming `json.Encoder` with marshal-then-write pattern — eliminates partial-write race on slow connections. ([@ashioyajotham](https://github.com/ashioyajotham))
- Security documentation hardened: shared secrets, key rotation, and deployment guidance expanded.

**Fixed**

- Broker: handle SQL `NULL` values for non-OAuth2 provider profiles — `api_key` and `basic_auth` providers no longer cause null pointer panics in the profile store.

</div>

---

<div class="changelog-release" markdown>

## 0.1.4 <span class="changelog-date">2026-04-01</span>

<div class="changelog-meta" markdown>
<div class="changelog-contributors">
<a href="https://github.com/sangalo20" title="Sangalo Mwenyinyo"><img src="https://github.com/sangalo20.png?size=32" alt="sangalo20"></a>
</div>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.1.4" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

**Added**

- Broker: `skip_scope_on_auth` provider parameter — bypasses strict scope validation on the authorization URL for providers that reject scope in the initial redirect (Salesforce).

</div>

---

<div class="changelog-release" markdown>

## 0.1.3 <span class="changelog-date">2026-04-01</span>

<div class="changelog-meta" markdown>
<div class="changelog-contributors">
<a href="https://github.com/sangalo20" title="Sangalo Mwenyinyo"><img src="https://github.com/sangalo20.png?size=32" alt="sangalo20"></a>
<a href="https://github.com/ashioyajotham" title="Victor Ashioya"><img src="https://github.com/ashioyajotham.png?size=32" alt="ashioyajotham"></a>
</div>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.1.3" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

**Added**

- Broker: validate `api_key` and `basic_auth` credentials before storing — rejects malformed or empty credentials at capture time rather than at retrieval.

**Fixed**

- Broker: enforce one token row per connection via upsert — eliminates duplicate token rows on reconnect. ([@ashioyajotham](https://github.com/ashioyajotham))
- Security: `ENCRYPTION_KEY` and `STATE_KEY` are now required at startup — Broker and Gateway fatal-exit with a clear message if either is absent. ([@ashioyajotham](https://github.com/ashioyajotham))
- Tests: `TestMain` used for binary lifecycle management; assertions refined.
- Gateway: `gofmt` formatting applied to main files.

</div>

---

<div class="changelog-release" markdown>

## 0.1.2 <span class="changelog-date">2026-04-01</span>

<div class="changelog-meta" markdown>
<div class="changelog-contributors">
<a href="https://github.com/sangalo20" title="Sangalo Mwenyinyo"><img src="https://github.com/sangalo20.png?size=32" alt="sangalo20"></a>
</div>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.1.2" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

**Fixed**

- Docker: corrected image names to `nexus-broker` and `nexus-gateway` — was using incorrect names that broke `docker pull` and Compose service references.

</div>

---

<div class="changelog-release" markdown>

## 0.1.1 <span class="changelog-date">2026-04-01</span>

<div class="changelog-meta" markdown>
<div class="changelog-contributors">
<a href="https://github.com/sangalo20" title="Sangalo Mwenyinyo"><img src="https://github.com/sangalo20.png?size=32" alt="sangalo20"></a>
<a href="https://github.com/Abdullahi254" title="Abdullahi Mohamud"><img src="https://github.com/Abdullahi254.png?size=32" alt="Abdullahi254"></a>
</div>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.1.1" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

**Added**

- Docker Hub publishing GitHub Actions workflow.
- Gateway: `capture-schema` and `capture-credential` proxy endpoints for static credential flows. ([@Abdullahi254](https://github.com/Abdullahi254))
- Open-core refactor: internal packages made public to support the OSS consumption model.

**Fixed**

- Go module paths updated to `github.com/Prescott-Data/nexus-framework` throughout.
- Broken database migration corrected.

</div>

---

<div class="changelog-release" markdown>

## 0.1.0 <span class="changelog-date">2026-02-19</span>

<div class="changelog-meta" markdown>
<div class="changelog-contributors">
<a href="https://github.com/sangalo20" title="Sangalo Mwenyinyo"><img src="https://github.com/sangalo20.png?size=32" alt="sangalo20"></a>
</div>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.1.0" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

Initial public release.

**Added**

- **Nexus Broker**: OAuth 2.0 and OIDC connection management — token storage (AES-GCM 256-bit at rest), background refresh loop, OIDC discovery with JWKS caching, nonce/id_token verification, Prometheus metrics.
- **Nexus Gateway**: public-facing API for agents and backends. Versioned at `/v1`. gRPC-first communication to Broker with REST fallback.
- **Nexus Bridge**: Go library for embedding in agent processes — `MaintainWebSocket` and `MaintainGRPCConnection` with automatic credential injection, token refresh, exponential backoff reconnection, and Prometheus metrics.
- **Go SDK** (`nexus-sdk`): zero-dependency HTTP client for the Gateway API.
- **Provider support**: Google (OIDC discovery), Azure AD (common tenant), GitHub, Salesforce, and arbitrary OAuth2 providers with manual endpoint configuration.
- **Security guardrails**: IP allowlisting (`ALLOWED_CIDRS`), allowed return domain validation, API key enforcement.
- **Docker Compose**: single `make up` command runs Broker, Gateway, PostgreSQL, and Redis.
- **Bitbucket Pipelines**: initial CI/CD configuration.

</div>
