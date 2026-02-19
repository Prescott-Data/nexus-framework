# Nexus Bridge

The **Nexus Bridge** is the official Go client library for the [Nexus Framework](https://github.com/Prescott-Data/nexus-framework). It acts as a "smart connector" that lives inside your agent or service, abstracting away the complexity of secure, persistent communication.

## Overview

Connecting to third-party APIs typically involves boilerplate for authentication (OAuth2 refreshes, API keys), resilience (retries, backoff), and state management. The Bridge handles this automatically by treating **Authentication as Data**.

It queries the **Nexus Gateway** for a "Strategy" (e.g., "Inject Bearer Token", "Sign with AWS SigV4") and executes it, ensuring your agent always has valid credentials without hardcoding provider logic.

### Key Features

- **Multi-Transport:** Native support for persistent **WebSocket** and **gRPC** connections.
- **Universal Auth:** Dynamically handles OAuth 2.0, API Keys, Basic Auth, and AWS SigV4.
- **Auto-Healing:** Implements exponential backoff and jitter for reconnection.
- **Proactive Refresh:** Refreshes tokens *before* they expire to prevent connection drops.
- **Observability:** Built-in structured logging (`slog`) and Prometheus metrics.

---

## Installation

```bash
go get github.com/Prescott-Data/nexus-framework/nexus-bridge
go get github.com/Prescott-Data/nexus-framework/nexus-sdk
```

---

## Usage Guide

### 1. Standard WebSocket Connection

This is the most common pattern for real-time agents (e.g., trading bots, chat interfaces).

```go
package main

import (
	"context"
	"log"
	"net/http"
	
	"github.com/Prescott-Data/nexus-framework/nexus-bridge"
	"github.com/Prescott-Data/nexus-framework/nexus-bridge/telemetry"
	"github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

// Define a handler for your WebSocket events
type myHandler struct{}

func (h *myHandler) OnConnect(send func([]byte) error) {
    log.Println("Connection established!")
    // You can send an initialization message here
    send([]byte(`{"type": "subscribe", "channel": "btc_usd"}`))
}

func (h *myHandler) OnMessage(msg []byte) {
    log.Printf("Received: %s", msg)
}

func (h *myHandler) OnDisconnect(err error) {
    log.Printf("Disconnected: %v", err)
}

func main() {
    ctx := context.Background()

    // 1. Initialize the SDK Client (talks to Gateway)
    authClient := oauthsdk.New("https://gateway.example.com")

    // 2. Initialize the Bridge with default telemetry
    // agentLabels are attached to all Prometheus metrics
    b := bridge.NewStandard(authClient, map[string]string{"agent": "bot-01"})

    // 3. Start the Connection Loop (Blocking)
    // The Bridge will fetch credentials for 'conn-123' and maintain the link.
    err := b.MaintainWebSocket(
        ctx,
        "conn-123",                 // Connection ID
        "wss://api.exchange.com/ws", // Target URL
        &myHandler{},
    )

    if err != nil {
        log.Fatalf("Permanent error: %v", err)
    }
}
```

### 2. Persistent gRPC Connection

The Bridge acts as a `PerRPCCredentials` provider for gRPC, injecting the correct metadata into every call and managing the underlying connection lifecycle.

```go
// Your logic loop. The Bridge calls this function every time it establishes
// a healthy, authenticated connection.
runLogic := func(ctx context.Context, conn *grpc.ClientConn) error {
    client := pb.NewMyServiceClient(conn)
    
    // Call the service as normal
    resp, err := client.GetData(ctx, &pb.Request{})
    if err != nil {
        return err // Returning an error triggers a Bridge reconnect
    }
    return nil
}

// Start the manager
b.MaintainGRPCConnection(
    ctx,
    "conn-456",
    "api.service.com:443",
    runLogic,
    grpc.WithTransportCredentials(credentials.NewTLS(nil)), // Secure transport
)
```

---

## Configuration Options

The `bridge.New` constructor accepts functional options to tune behavior.

| Option | Description | Default |
| :--- | :--- | :--- |
| `WithLogger(l Logger)` | Custom logger implementation. | `nopLogger` (Silent) |
| `WithMetrics(m Metrics)` | Custom metrics collector. | `nopMetrics` (Silent) |
| `WithRetryPolicy(p)` | Configures backoff/jitter. | 2s min, 30s max |
| `WithRefreshBuffer(d)` | How early to refresh tokens. | `5m` |
| `WithPingInterval(d)` | WebSocket Ping frequency. | `30s` |
| `WithWriteTimeout(d)` | WebSocket Write deadline. | `10s` |

**Example:**

```go
b := bridge.New(
    client,
    bridge.WithRefreshBuffer(10 * time.Minute), // Aggressive refresh
    bridge.WithRetryPolicy(bridge.RetryPolicy{
        MinBackoff: 100 * time.Millisecond,     // Fast retry
        MaxBackoff: 5 * time.Second,
        Jitter:     100 * time.Millisecond,
    }),
)
```

---

## Telemetry

The `NewStandard` constructor enables built-in telemetry compatible with cloud-native stacks.

### Metrics (Prometheus)
Expose the `/metrics` endpoint in your application to scrape these:

- `nexus_bridge_connections_total`: Total successful connections.
- `nexus_bridge_disconnects_total`: Total connection drops.
- `nexus_bridge_token_refreshes_total`: Auth refreshes performed.
- `nexus_bridge_connection_status`: 1 = Connected, 0 = Disconnected.

```go
http.Handle("/metrics", telemetry.Handler())
go http.ListenAndServe(":9090", nil)
```

### Logging (Slog)
The default logger outputs JSON-formatted logs to `stdout`, suitable for ingestion by Datadog, Splunk, or CloudWatch.

```json
{"time":"...","level":"INFO","msg":"Reconnecting","connectionID":"...","after":"2s"}
```
