# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.1] - 2026-05-10

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
