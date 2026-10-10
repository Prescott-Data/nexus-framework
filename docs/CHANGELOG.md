---
icon: material/history
hide:
  - toc
---

# Changelog

All notable changes to Nexus are documented here. This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

<div class="changelog-release" markdown>

## 0.6.0 <span class="changelog-date">2026-10-10</span>

<div class="changelog-meta" markdown>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.6.0" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

**Added**

- **The Gateway can require an API key from its callers.** Until now the Gateway authenticated only itself to the Broker (`BROKER_API_KEY`) and checked nothing on the way in, so anyone who could reach it could list providers and mint tokens; the `REQUIRE_API_KEY` the Compose file handed it was never read. With `GATEWAY_REQUIRE_API_KEY=true`, every route except `/health`, `/auth/callback`, `/v1/capture-schema` and `/v1/capture-credential` (the ones a browser or a load balancer reaches) requires `X-API-Key` to match one of `GATEWAY_API_KEYS` or a mounted `GATEWAY_API_KEY_FILE` / `GATEWAY_API_KEYS_FILE`, reloaded every `GATEWAY_API_KEY_RELOAD_INTERVAL` (30s) so keys rotate without a restart. Missing key is 401, wrong key is 403; the gRPC server applies the same check on `x-api-key` metadata. Off by default for existing deployments, with a loud startup warning. Enabling it with no keys configured refuses to start.
- **The Go SDK can send the key.** New `WithAPIKey(key)` option, the counterpart of the TypeScript and Python SDKs' `apiKey`; it is sent as `X-API-Key` on every Gateway call. `WithBroker` is unchanged and stays the Broker credential. The Sidecar passes `GATEWAY_API_KEY` through it. The Gateway OpenAPI contract declares the `ApiKeyAuth` scheme on every operation.

**Changed**

- `docker-compose.yml` no longer passes the Gateway `REQUIRE_API_KEY`, `REQUIRE_ALLOWLIST`, `ALLOWED_CIDRS` and `ALLOWED_RETURN_DOMAINS`, which it never read; it passes `GATEWAY_REQUIRE_API_KEY` and `GATEWAY_API_KEYS` instead.

</div>

---

<div class="changelog-release" markdown>

## 0.5.0 <span class="changelog-date">2026-10-10</span>

<div class="changelog-meta" markdown>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.5.0" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

**Added**

- **Provider client secrets can live in GCP Secret Manager.** `SECRET_BACKEND=gcp-secret-manager` on the Broker stores each OAuth2 provider's `client_secret` as its own Secret Manager secret, named `<GCP_SECRET_PREFIX>-<provider id>`, and leaves `provider_profiles.client_secret` NULL. Registration rolls back if the backend write fails, `PATCH` rotates the secret and destroys earlier versions, and deleting a provider deletes its secret. The default `internal` backend is unchanged, and rows that still carry a column value keep working after the switch. The Broker checks the backend at startup and refuses to boot if it cannot reach it. A Terraform module in `deploy/terraform/gcp/secret-backend` grants the Broker's service account create on the project plus admin on prefixed secrets only. First backend for #63; the `pkg/vault/providers.Backend` interface is where Vault and AWS go next.

**Changed**

- The token encryption helpers moved from `pkg/vault` to `pkg/vault/tokens`, so that `pkg/vault` can hold both the token vault and the provider secret backends. `vault.Encrypt` and `vault.Decrypt` remain as deprecated wrappers. No behaviour change.

</div>

---

<div class="changelog-release" markdown>

## 0.4.3 <span class="changelog-date">2026-10-10</span>

<div class="changelog-meta" markdown>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.4.3" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

**Changed**

- **One version everywhere, enforced.** The `VERSION` file was still 0.3.1 while v0.4.0 and v0.4.1 had been tagged by hand through the GitHub Releases UI, so binaries, images, docs and SDK manifests disagreed about what version they were. This release reconciles them: `VERSION` is bumped past the stray tags, and `make check-version` now fails a pull request when any stamped file, the top changelog heading, or the newest git tag disagrees with `VERSION`. Stamping covers the TypeScript and Python SDK manifests as well as the OpenAPI specs and docs site. Main is protected: changes land by pull request with CI green, and a tag ruleset blocks `v*` tags from everyone but the release workflow, which pushes with a deploy key. The container image build reads `VERSION` rather than the git ref and refuses a tag that disagrees with it. See the Versioning section of `CONTRIBUTING.md`.

**Fixed**

- The changelog now has a section for every release since 0.2.0. The sections for 0.2.3 through 0.2.7, 0.3.0, 0.3.1 and 0.4.1 were missing or filed as "Unreleased".

</div>

---

<div class="changelog-release" markdown>

## 0.4.1 <span class="changelog-date">2026-09-30</span>

<div class="changelog-meta" markdown>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.4.1" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

**Fixed**

- **Providers that take a JSON token-exchange body are supported.** Set `token_request_format: json` in the provider's `params` and the Broker sends the authorization-code exchange as a JSON object instead of a form, which Notion and other spec-deviating providers require. The parameter is kept out of the authorization URL. (#111)

**Changed**

- README rewritten: hero, architecture diagram, examples in all three SDK languages, and an FAQ.

</div>

---

<div class="changelog-release" markdown>

## 0.4.0 <span class="changelog-date">2026-09-16</span>

<div class="changelog-meta" markdown>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/commits/main" target="_blank" rel="noopener noreferrer">View commits on GitHub →</a>
</div>

**Added**

- **Connections can be revoked.** `DELETE /v1/connections/{id}` on the Gateway (`DELETE /connections/{id}` on the Broker) permanently terminates a connection: it attempts an RFC 7009 revocation at the provider, deletes the encrypted credential, closes every open agent session bound to the connection, and moves it to the terminal `revoked` status. Until now a stored credential had no off-switch — it lived in the database until the connection was superseded by a fresh consent — which left no answer for an offboarded user, a leaked token, or a deletion request.

    The local credential is destroyed even when the upstream call fails, so a provider outage can never leave a live token behind — local teardown runs on its own deadline, independent of the upstream call. The response reports `token_deleted` and `provider_revoked` separately, because "Nexus can no longer use this token" and "this token is dead at the provider" are different guarantees and incident response needs to tell them apart. Providers that do not advertise a revocation endpoint, and static `api_key`/`basic_auth` credentials, are destroyed locally only and say so in `provider_revocation_error`.

    Revocation is idempotent, and a retry reports the *original* outcome — the stored `revoked_at` and upstream result — so a client retrying after a timeout gets a confirmable answer rather than a fresh-looking one. Passing `workspace_id` scopes the request: a mismatch returns 404 rather than 403, so the endpoint does not confirm the existence of connection IDs to a caller that does not own them. `reason` is recorded on the connection and in the new `connection.revoked` audit event.

    Available in all three SDKs as `RevokeConnection` / `revokeConnection` / `revoke_connection`.

- **`make test-integration` runs tests against a real PostgreSQL instance.** These live behind the `integration` build tag and are excluded from `make test`, which has no database. Point `NEXUS_TEST_DATABASE_URL` at a migrated database to run them. Revocation is the first feature covered: transaction boundaries, the agent-session cascade and the `revoked_at` columns are all things sqlmock will accept but a real server can reject.

**Changed**

- **Requesting a token or a refresh for a revoked connection returns `410 Gone`** with code `connection_revoked`, rather than the generic `400 connection_not_active`. A revoked connection is never coming back, and a client must start a fresh connection flow instead of retrying. The Gateway now forwards the Broker's error body on `/v1/token`, `/v1/refresh` and `/v1/connections/{id}`, so the code actually reaches the caller instead of arriving as a bodyless status.
- **Revocation is terminal against concurrent writers.** Credential writes take a `FOR SHARE` lock on the connection row and are gated on `revoked_at IS NULL`, so a refresh, OAuth exchange or static capture that was in flight when a revocation committed is rejected rather than recreating the credential that was just destroyed.
- `ConnectionSummary` now carries `revoked_at`.

</div>

---

<div class="changelog-release" markdown>

## 0.3.1 <span class="changelog-date">2026-09-10</span>

<div class="changelog-meta" markdown>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.3.1" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

**Fixed**

- **Explicit OAuth token endpoints are honored.** When a provider profile sets `token_url` alongside an `issuer`, the Broker exchanges and refreshes against the configured `token_url` rather than the discovered one. (#106)

</div>

---

<div class="changelog-release" markdown>

## 0.3.0 <span class="changelog-date">2026-09-07</span>

<div class="changelog-meta" markdown>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/releases/tag/v0.3.0" target="_blank" rel="noopener noreferrer">View release on GitHub →</a>
</div>

**Added**

- **The Broker applies its own migrations on boot.** The image carries `migrations/`, and the Broker applies whatever the database has not recorded in a new `schema_migrations` ledger, keyed by filename. Running twice is a no-op, and a partly-migrated database continues from where it stopped. Set `AUTO_MIGRATE=false` where migrations are run as a separate step, and `MIGRATIONS_DIR` to relocate them. (#101, #102)
- **SAML 2.0 service-provider flow**, **agent identity sessions** (`/v1/agents`, `/v1/agent-sessions`), the **nexus-sidecar** proxy, bridge metrics and Broker API key rotation via `API_KEY_FILE` / `API_KEYS_FILE`.

**Fixed**

- **Migrations are replayable.** `ADD COLUMN`, `CREATE TABLE`, `CREATE INDEX` and the `tokens_connection_id_unique` constraint are guarded, so a database adopting the ledger does not fail on migrations whose effect is already present. (#102)
- Two migrations no longer share the prefix `11`; they are now `11a_add_provider_description.sql` and `11b_add_audit_created_at_index.sql`. (#103)
- `make stamp` works on macOS; it relied on GNU-only `sed` behaviour and silently stamped nothing on BSD sed.
- SAML `return_url` is validated before credentials are stored. The sidecar returns `413 body_too_large` for oversized bodies. Bridge WebSocket duration is measured at exit. Dependency bumps for `x/net`, `x/crypto`, `grpc` and `goxmldsig` security advisories.

</div>

---

<div class="changelog-release" markdown>

## 0.2.3 – 0.2.7 <span class="changelog-date">2026-05-14 to 2026-07-30</span>

<div class="changelog-meta" markdown>
<div class="changelog-contributors">
<a href="https://github.com/sangalo20" title="Sangalo Mwenyinyo"><img src="https://github.com/sangalo20.png?size=32" alt="sangalo20"></a>
<a href="https://github.com/ekizito96" title="Muyukani Ephraim Kizito"><img src="https://github.com/ekizito96.png?size=32" alt="ekizito96"></a>
</div>
<a class="changelog-release-link" href="https://github.com/Prescott-Data/nexus-framework/commits/main" target="_blank" rel="noopener noreferrer">View commits on GitHub →</a>
</div>

_This section was filed as "Unreleased" at the time and was never split. It covers v0.2.3 (2026-05-14, the SDKs and release workflow below), v0.2.4 (2026-05-19, provider and connection health checks with `--worker-only` mode, bounded worker concurrency, `GET /connections?workspace_id=`, `health_status` on token responses), v0.2.5 (2026-07-14, versioning unified on the root `VERSION` file, `refresh_token` preserved on refresh, `client_secret` redacted from responses, expired OAuth2 tokens auto-refreshed, static credentials fail closed when unvalidatable), v0.2.6 (2026-07-29, self-hosted base URL and path-based credential validation, provider manifest for static-credential rollout, CodeQL fixes) and v0.2.7 (2026-07-30, credential validation never served from the response cache, non-public targets blocked for validation probes)._


**Added**

- **Python SDK** (`nexus-sdk-python`): full-feature-parity Python client — `get_token_by_connection_id`, `resolve_token`, `request_connection`, `check_connection`. Zero external dependencies.
- **TypeScript SDK** (`@dromos/nexus-sdk`): evolved from `nexus-mcp-adapter`. MCP token resolution, in-memory caching, authenticated transport, and `resolveToken` for stateless MCP clients. Built to `dist/`, ESM imports hardened.
- **Go SDK MCP integration**: `ResolveToken` endpoint for stateless MCP clients — workspace and provider-scoped token resolution with TTL caching.
- **Multi-strategy credential support** across all three SDKs: handles `oauth2`, `api_key`, `basic_auth`, `aws_sigv4`, `query_param`, and `hmac_payload` strategies without caller-side branching.
- **Automated release workflow**: GitHub Actions CI/CD pipeline, bumped `VERSION` to `0.2.3`.
- **Agent auth proposal** (`AGENT_AUTH_PROPOSAL.md`): full design document for agent identity, OBO sessions, scoped session TTLs, and custom scope enforcement.
- **SDK documentation**: comprehensive reference pages for Go, TypeScript, and Python SDKs including install, method signatures, MCP integration examples, and error handling.

**Fixed**

- **Broker OAuth endpoint selection:** explicit provider `auth_url` and
  `token_url` values are now authoritative when `enable_discovery=false`.
  Previously, token exchange still attempted OIDC discovery and could replace a
  provider's configured endpoint; for Slack this exchanged the authorization
  code against the OpenID endpoint and stored an ID token without the bot access
  token.
- **Broker: static credentials are now validated before activation.** `api_key`/`basic_auth` connections fail closed (`provider_not_validatable`) when the provider has no `api_base_url` + `user_info_endpoint`, instead of accepting any key and reporting `active`. When configured, a `401`/`403` from the provider rejects the key with `invalid_credentials`.
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
