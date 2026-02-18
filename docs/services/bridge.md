# Nexus Bridge

The **Nexus Bridge** is a Go library that simplifies the usage of the Nexus Framework. It sits between your Agent's business logic and the external Provider's API.

## Core Features

### 1. Strategy-Based Signing
The Bridge is "dumb" by design—it doesn't need to know *how* to sign an AWS request or an OAuth request. Instead, it:
1.  Fetches a **Strategy** from the Gateway.
2.  Interprets the strategy (e.g., "Inject into header 'X-API-Key'").
3.  Applies the credentials to your `http.Request`.

### 2. Persistent gRPC Connections
The Bridge provides a `MaintainGRPCConnection` helper that:
- Implements the gRPC `PerRPCCredentials` interface.
- Automatically handles the handshake and token injection for every RPC call.
- Retries the connection with exponential backoff if it drops.

### 3. Managed WebSocket Lifecycle
The `MaintainWebSocket` helper handles:
- Background token refreshes: It refreshes the token *before* it expires to prevent connection drops.
- Pong/Ping health checks.
- Graceful reconnection logic.

### 4. Telemetry
The Bridge includes built-in support for:
- **Structured Logging:** Via the `Slog` package.
- **Prometheus Metrics:** Tracks connection status, token refresh success/failure, and active connections.

## Usage Example (HTTP)

```go
import "dromos.io/nexus-bridge/internal/auth"

// ... inside your agent ...
token, _ := sdkClient.GetToken(ctx, connectionID)

req, _ := http.NewRequest("GET", "https://api.provider.com/data", nil)

// The Bridge handles the signing logic dynamically
auth.ApplyAuthentication(req, token.Strategy, token.Credentials)

resp, _ := httpClient.Do(req)
```
