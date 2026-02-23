# Nexus Framework Release Notes (v0.1.0-rc1)

This release marks a significant stabilization effort for the Nexus Framework, focusing on security hardening, deployment reliability, and build system compatibility.

## 🚨 Breaking Changes

*   **Go Version Requirement:** `nexus-bridge` has been downgraded to Go 1.23.0 for broader compatibility. Ensure your local environment is running Go 1.23+.
*   **Startup Validation:** The Broker and Gateway services will now **fail to start** in production environments if `STATE_KEY` is not explicitly set. This prevents accidental deployment of insecure configurations.

## 🛡️ Security Fixes

*   **Gateway CORS:** Restricted Cross-Origin Resource Sharing (CORS) in the Gateway. Use the new `CORS_ALLOWED_ORIGINS` environment variable to whitelist specific domains (e.g., `https://app.example.com`).
*   **Secure Defaults:** Hardened the random key generation logic for development environments while enforcing strict requirements for production.

## 🏗️ Architecture & Refactoring

*   **Decoupling:** Removed provider-specific (Azure) logic from the Gateway, reinforcing its role as a provider-agnostic proxy.
*   **Code Quality:** Replaced magic strings in the Broker with a centralized `models` package for Auth Types and Connection Statuses.
*   **Dependency Cleanup:**
    *   Upgraded `nexus-broker` Redis client to v9.
    *   Fixed `nexus-bridge` dependency timestamps and versions.
    *   Corrected import paths in `nexus-gateway` to ensure clean builds.

## 📚 Documentation

*   **Deployment Guide:** Added critical configuration details for `CORS_ALLOWED_ORIGINS` and `STATE_KEY`.
*   **Community Standards:** Updated `CODE_OF_CONDUCT.md` and `LICENSE` with correct contact information and copyright details.
*   **API Reference:** Fixed relative links to ensure compatibility with MkDocs site generation.

## 📦 Component Updates

### Nexus Broker
- `cmd/nexus-broker`: Enforced `STATE_KEY` check.
- `internal/models`: New package for shared constants.
- `internal/handlers`: Refactored callback and consent logic.
- `go.mod`: Upgraded dependencies.

### Nexus Gateway
- `cmd/nexus-grpc` & `cmd/nexus-rest`: Enforced `STATE_KEY` check.
- `internal/grpc`: Implemented configurable CORS middleware.
- `internal/usecase`: Removed hardcoded Azure scopes.

### Nexus Bridge
- `go.mod`: Downgraded Go version to 1.23.0.
