# Nexus Framework Code Review

## Architecture Overview
The Nexus Framework is a distributed identity orchestration system composed of three main parts:
- **Nexus Broker**: The core backend service (Go) managing provider profiles, connections, and token storage. It uses PostgreSQL for persistence and Redis for caching. It exposes a REST API.
- **Nexus Gateway**: A proxy service (Go) exposing both gRPC and REST interfaces. It handles client requests and forwards them to the Broker, managing protocol translation and some state verification.
- **Nexus Bridge**: A client-side library (Go) for agents to maintain persistent connections (WebSocket/gRPC) with the framework.

---

## 1. Critical Blockers
*Must be resolved before public release.*

### 1. Security: Unrestricted CORS in Gateway
- **File**: `nexus-gateway/internal/grpc/server_grpc.go` (Line ~136)
- **Issue**: The CORS configuration `AllowedOrigins: []string{"https://*", "http://*"}` allows any origin to access the Gateway API.
- **Risk**: This permits malicious websites to interact with the Gateway if a user is authenticated (or via network position), potentially triggering unwanted connection flows.
- **Fix**: Remove the wildcard and make allowed origins configurable via an environment variable (e.g., `CORS_ALLOWED_ORIGINS`).

### 2. Deployment: Implicit Shared State Key Dependency
- **Files**: `nexus-broker/cmd/nexus-broker/main.go`, `nexus-gateway/cmd/nexus-grpc/main.go`, `nexus-gateway/cmd/nexus-rest/main.go`
- **Issue**: Both services independently generate a random `STATE_KEY` if the environment variable is missing.
- **Risk**: In a distributed deployment, or even when running separate processes locally without explicit config, the Gateway will fail to verify the `state` parameter signed by the Broker (or vice versa) because the keys will differ. This leads to confusing "Invalid state" errors.
- **Fix**:
    1.  Fail startup if `STATE_KEY` is missing in production mode.
    2.  Explicitly document that this key *must* be shared between services.

### 3. Build: Broken Dependencies in Bridge
- **File**: `nexus-bridge/go.mod`
- **Issue**: The file specifies `go 1.25.3` (a future version) and dependencies with timestamps from late 2025.
- **Risk**: The project will fail to build on standard CI/CD pipelines and developer machines.
- **Fix**: Downgrade to a stable Go version (e.g., 1.23 or 1.24) and run `go mod tidy` to fix dependency versions.

---

## 2. Refactoring Opportunities
*Should be addressed to improve maintainability.*

### 1. Tight Coupling in Gateway
- **File**: `nexus-gateway/internal/usecase/handler.go`
- **Issue**: `RequestConnectionCore` contains hardcoded logic for "azure" provider scopes (`"openid"`, `"email"`, `"profile"`, `"offline_access"`).
- **Impact**: This violates the "provider-agnostic" goal. If Azure changes requirements or a new provider needs similar handling, code changes in the Gateway are required.
- **Recommendation**: Move this logic to the Broker, potentially using the `params` JSON field in `provider_profiles` to define default or forced scopes.

### 2. Magic Strings & Duplication
- **Files**: `nexus-broker/internal/provider/store.go`, `nexus-broker/internal/handlers/callback.go`
- **Issue**: Hardcoded strings like `"oauth2"`, `"api_key"`, `"active"`, and `"pending"` are scattered across the codebase.
- **Impact**: Increases the risk of typos and makes refactoring (e.g., renaming a status) difficult.
- **Recommendation**: Define these constants in a shared `pkg/types` or `internal/models` package.

### 3. Duplicate Entry Points
- **Files**: `nexus-gateway/cmd/nexus-grpc/main.go` vs `nexus-gateway/cmd/nexus-rest/main.go`
- **Issue**: Significant logic duplication for configuration loading and setup.
- **Recommendation**: Unify into a single `nexus-gateway` binary that can start either or both servers based on flags/config.

---

## 3. Documentation Gaps

- **State Key Requirement**: The `README.md` mentions `nexus-admin-key` but omits the critical `STATE_KEY` sharing requirement.
- **API Key Configuration**: The `API_KEYS` (comma-separated) environment variable support in `nexus-broker` (`apikey.go`) is not documented.
- **SDK Typing**: `nexus-sdk` uses `map[string]any` heavily. `TokenResponse` should have stronger types for fields like `ExpiresAt` to improve the developer experience.

---

## 4. Quick Wins

- **Update Redis Client**: Upgrade `nexus-broker` from `github.com/go-redis/redis/v8` (v8.11.5) to v9.
- **Linting**: Run `go mod tidy` in `nexus-broker` to remove unused dependencies.
- **Logging**: Standardize logging. Currently, a mix of `log` (std lib) and `middleware.Logger` is used. Adopting `slog` (Go 1.21+) would provide structured logging out of the box.
