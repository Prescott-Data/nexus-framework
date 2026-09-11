# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Connections can be revoked.** `DELETE /v1/connections/{id}` on the Gateway
  (`DELETE /connections/{id}` on the Broker) permanently terminates a
  connection: it attempts an RFC 7009 revocation at the provider, deletes the
  encrypted credential, closes every open agent session bound to the
  connection, and moves it to the terminal `revoked` status. Until now a stored
  credential had no off-switch — it lived in the database until the connection
  was superseded by a fresh consent — which left no answer for an offboarded
  user, a leaked token, or a deletion request.

  The local credential is destroyed even when the upstream call fails, so a
  provider outage can never leave a live token behind. The response reports
  `token_deleted` and `provider_revoked` separately, because "Nexus can no
  longer use this token" and "this token is dead at the provider" are different
  guarantees and incident response needs to tell them apart. Providers that do
  not advertise a revocation endpoint, and static `api_key`/`basic_auth`
  credentials, are destroyed locally only and say so in
  `provider_revocation_error`.

  Revocation is idempotent, so a client retrying after a timeout still gets a
  confirmable result. Passing `workspace_id` scopes the request: a mismatch
  returns 404 rather than 403, so the endpoint does not confirm the existence
  of connection IDs to a caller that does not own them. `reason` is recorded on
  the connection and in the new `connection.revoked` audit event.

  Available in all three SDKs as `RevokeConnection` / `revokeConnection` /
  `revoke_connection`.

### Changed
- **Requesting a token for a revoked connection returns `410 Gone`** with code
  `connection_revoked`, rather than the generic `400 connection_not_active`. A
  revoked connection is never coming back, and a client must start a fresh
  connection flow instead of retrying.
- `ConnectionSummary` now carries `revoked_at`.
- **`make test-integration` runs tests against a real PostgreSQL instance.**
  These live behind the `integration` build tag and are excluded from
  `make test`, which has no database. Point `NEXUS_TEST_DATABASE_URL` at a
  migrated database to run them. Revocation is the first feature covered:
  transaction boundaries, the agent-session cascade and the `revoked_at`
  columns are all things sqlmock will accept but a real server can reject.

## [0.3.0] - 2026-09-07

### Added
- **The broker applies its own migrations on boot.** The image now carries
  `migrations/`, and the broker applies whatever the database has not recorded
  in a new `schema_migrations` ledger, keyed by filename. Running twice is a
  no-op, and a partly-migrated database continues from where it stopped. Set
  `AUTO_MIGRATE=false` where migrations are run as a separate step, and
  `MIGRATIONS_DIR` to relocate them. (#101, #102)

### Fixed
- **Migrations are replayable.** `ADD COLUMN`, `CREATE TABLE`, `CREATE INDEX`
  and the `tokens_connection_id_unique` constraint are now guarded, so a
  database adopting the ledger does not fail on migrations whose effect is
  already present. Previously a deployment that had applied migrations by hand
  could not start the new broker at all. (#102)
- **Two migrations no longer share the prefix `11`**, which would have caused
  any version-keyed runner to apply one and skip the other permanently. They
  are now `11a_add_provider_description.sql` and
  `11b_add_audit_created_at_index.sql`, preserving their order. (#103)
- **`make stamp` works on macOS.** It relied on GNU-only `sed -i -E` and the
  `0,/re/` address, so on BSD sed it wrote files named `-E` and stamped nothing,
  while reporting success. A contributor on macOS could not pass the version
  consistency check.
- **Static credential validation now fails closed**: `api_key`/`basic_auth` connections are no longer marked `active` when the provider has no `api_base_url` + `user_info_endpoint` to validate against — capture now returns `provider_not_validatable` instead of accepting any key. When a validation endpoint is configured, a `401`/`403` from the provider rejects the key with `invalid_credentials`.

## [0.2.4] - 2026-05-19

### Added
- **Health Check Hardening**: Provider health cross-referencing prevents mass-expiration of connections during transient upstream outages.
- **Bounded Concurrency**: Semaphore + WaitGroup pattern limits goroutine growth in both `HealthWorker` (max 10) and `ConnectionHealthWorker` (max 20).
- **Graceful Shutdown**: `--worker-only` mode now handles `SIGINT`/`SIGTERM` for clean process lifecycle management.
- **Frontend API**: New `GET /connections?workspace_id=` endpoint returns workspace-scoped connection summaries with health status.
- **Token Health Status**: `GET /connections/{id}/token` response now includes `health_status` field.
- **Database Index**: Partial index on `connections(status, last_health_check_at)` optimizes health check polling at scale.

### Fixed
- `GET /providers/health` returns `[]` instead of `null` for empty provider lists.
- Standardized logging: replaced `fmt.Printf` with `log.Printf` in background workers.

---

### Changed
- **Service Layer**: Refactored `connection_part2.go` into `credential.go`, separating credential capture, token refresh, and credential validation by responsibility.
- **HTTP Client**: `validateCredentials`, `refreshTokens`, and `executeExchange` now use the centrally injected `httpClient` instead of creating inline clients, ensuring the configured transport is respected across all outbound calls.
- **Audit Interface**: `ConnectionService` now accepts the `audit.Logger` interface instead of a concrete `*audit.Service` pointer, enabling proper mocking in unit tests.
- **Method Promotion**: `validateCredentials` and `refreshTokens` promoted from standalone functions to methods on `connectionService` to allow struct field access.

### Added
- **Service Layer Tests**: 7 new unit tests covering the previously untested `SaveCredential`, `Refresh`, and `ExchangeCodeForTokens` methods, including OAuth2 flows validated against `httptest` mock servers.
- **SOC 2 Integration Tests**: Enterprise-grade compliance test suite (`soc_test.go`, `soc_livedb_test.go`) verifying encryption at rest (SOC-CTRL-01), immutable audit trail (SOC-CTRL-02), API key enforcement (SOC-CTRL-03), IP allowlisting (SOC-CTRL-04), and defense-in-depth middleware (SOC-CTRL-05).
- **Architecture Enforcement**: `TestSeparationOfConcerns` statically analyzes import paths via `go/parser` to enforce layer boundaries at CI time.
- **Docker Compose**: Local PostgreSQL and Redis containers for running live integration tests against a real database schema.

---

## [0.2.0] - 2026-05-05

### Added
- **Security-as-Code CLI**: Declarative provider manifest management via YAML (`nexus apply`, `nexus plan`, `nexus diff`), with field-level diff output and concurrent provider fetching.
- **Audit Subsystem**: Structured audit event logging to `audit_events` table with caller IP, User-Agent, and JSON event data.
- **Secret Masking**: CLI masks sensitive fields in plan output to prevent credential exposure in logs.

### Changed
- **CI/CD**: Removed CI workflow from the open repository; internal Azure deployment pipeline secured behind manual trigger.
- **Documentation**: All registry examples standardized to `localhost:8090` to support OSS adoption without exposing internal infrastructure.
- **Providers Endpoint**: Fixed path references (`/v1/providers` → `/providers`, `/v1/audit` → `/audit`) throughout documentation and code.

---

## [0.1.0] - 2026-02-19

### Added
- **Nexus Broker**: Core service for managing OAuth 2.0 and OIDC connections.
- **Nexus Gateway**: Public-facing API gateway for agents.
- **Nexus Bridge**: Go library for integrating agents with the Nexus framework.
- **Documentation**: Comprehensive guides for architecture, deployment, and integration.
- **Versioning**: Centralized version management via `VERSION` file.

### Changed
- Initial project release.
