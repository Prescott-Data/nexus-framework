# Nexus Bridge

The **Nexus Bridge** is a purpose-built Go library designed to simplify the complexity of connecting agents to external APIs. It acts as the "smart connector" that lives inside your agent's process, abstracting away the heavy lifting of authentication, network resilience, and state management.

## 1. Why use the Bridge?

Connecting to third-party APIs often involves boilerplate that distracts from core business logic:
- **Authentication:** Handling OAuth 2.0 token refreshes, calculating AWS SigV4 signatures, or managing API key headers.
- **Resilience:** Implementing exponential backoff for dropped connections.
- **Maintenance:** Ensuring long-lived connections (like WebSockets) stay alive for days or weeks.

The Bridge solves this by treating **Authentication as Data**. It asks the Nexus Gateway "How do I connect to this?", and receives a **Strategy** and **Credentials**. It then executes that strategy automatically.

## 2. Installation

```bash
go get github.com/Prescott-Data/nexus-framework/nexus-bridge
go get github.com/Prescott-Data/nexus-framework/nexus-sdk
```

## 3. Core Concepts

### The Strategy Engine
Unlike traditional SDKs that are hardcoded for specific providers (e.g., "The GitHub Client"), the Bridge is generic. It supports a wide range of authentication strategies dynamically:

| Strategy | Description |
| :--- | :--- |
| `oauth2` | Standard Bearer token injection. Automatically refreshes tokens before expiry. |
| `api_key` / `header` | Injects a value into a specific header (e.g., `X-API-Key`). |
| `basic_auth` | Standard HTTP Basic Auth (Base64 user:pass). |
| `query_param` | Appends a value to the URL query string (e.g., `?key=...`). |
| `aws_sigv4` | Calculates AWS Signature Version 4 for calls to AWS services. |

### The "Universal Connector"
Because the Bridge relies on strategies returned by the Broker, you can write a single piece of code that connects to Google, Stripe, and AWS without changing your application logic. You simply pass a different `connection_id`.

## 4. Supported Transports

The Bridge provides high-level helpers for the three most common communication patterns.

### A. Persistent WebSocket
Ideal for real-time feeds (market data, chat, notifications).

**Features:**
- **Automatic Reconnection:** Uses exponential backoff with jitter to reconnect after network failures.
- **Proactive Refresh:** Monitors the token's expiration and performs a refresh *while the connection is open*, preventing "token expired" disconnects where possible.
- **Health Checks:** Handles Ping/Pong frames automatically.

```go
// Your handler implementation
type Handler struct{}
func (h *Handler) OnConnect(send func([]byte) error) { /* Init logic */ }
func (h *Handler) OnMessage(msg []byte)               { /* Process msg */ }
func (h *Handler) OnDisconnect(err error)             { /* Cleanup */ }

// Usage
b.MaintainWebSocket(ctx, connectionID, "wss://api.provider.com/feed", &Handler{})
```

### B. Persistent gRPC
Ideal for high-performance service-to-service communication.

**Features:**
- **Credential Injection:** Implements gRPC's `PerRPCCredentials` interface to inject auth metadata into every call.
- **Smart Retries:** Wraps your client logic in a loop that re-dials if the underlying connection enters a failure state.

```go
runLogic := func(ctx context.Context, conn *grpc.ClientConn) error {
    client := pb.NewServiceClient(conn)
    // ... use client ...
    return nil // Logic loop finished
}

b.MaintainGRPCConnection(ctx, connectionID, "api.provider.com:443", runLogic, grpc.WithTransportCredentials(creds))
```

### C. Standard HTTP (Manual)
For simple REST calls, you can use the internal `auth` package to sign standard Go `http.Request` objects.

```go
import "github.com/Prescott-Data/nexus-framework/nexus-bridge/internal/auth"

// 1. Get the token/strategy from the Gateway
token, _ := sdkClient.GetToken(ctx, connectionID)

// 2. Create your request
req, _ := http.NewRequest("GET", "https://api.provider.com/resource", nil)

// 3. Apply credentials (modifies req.Header or req.URL)
auth.ApplyAuthentication(req, token.Strategy, token.Credentials)

// 4. Send
resp, _ := httpClient.Do(req)
```

## 5. Telemetry & Observability

The Bridge is "Production Ready" by default. It emits structured logs and Prometheus metrics.

### Prometheus Metrics
When using `NewStandard(...)`, the following metrics are registered:

- `nexus_bridge_connections_total`: Counter of successful connection establishments.
- `nexus_bridge_disconnects_total`: Counter of connection drops.
- `nexus_bridge_token_refreshes_total`: Counter of token refresh operations.
- `nexus_bridge_connection_status`: Gauge (0 or 1) indicating if the agent is currently connected.

### Logging
The Bridge uses a generic `Logger` interface. `NewStandard` defaults to Go's structured logger (`slog`). You can inject your own logger (Zap, Logrus) by implementing the simple interface:

```go
type Logger interface {
    Info(msg string, keysAndValues ...interface{})
    Error(err error, msg string, keysAndValues ...interface{})
}
```

## 6. Configuration Options

The Bridge uses the Functional Options pattern for configuration.

```go
b := bridge.New(
    authClient,
    // Custom Logger
    bridge.WithLogger(myLogger),
    
    // Custom Metrics
    bridge.WithMetrics(myMetrics),
    
    // Fine-tune Retry Policy
    bridge.WithRetryPolicy(bridge.RetryPolicy{
        MinBackoff: 100 * time.Millisecond,
        MaxBackoff: 10 * time.Second,
        Jitter:     50 * time.Millisecond,
    }),
    
    // Adjust buffer for proactive refresh (default 5m)
    bridge.WithRefreshBuffer(2 * time.Minute),
)
```
