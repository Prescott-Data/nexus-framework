# The Bridge

The Bridge is a Go library you embed in your agent process. It maintains an authenticated connection, fetching tokens, injecting credentials into requests, and refreshing tokens before expiry, so your agent code handles only application logic.

## How it works

When you call `MaintainWebSocket` or `MaintainGRPCConnection`, the Bridge runs a `manageConnection` loop:

1. Calls `GET /v1/token/{connection_id}` to fetch the current token and auth strategy.
2. Applies the strategy to authenticate the underlying connection (WebSocket headers or gRPC metadata).
3. Starts three goroutines: a read pump, a write pump, and a ping ticker.
4. Monitors `ExpiresAt` and fires a background `RefreshConnection` before the token expires. The connection stays open during refresh.
5. On any error, calls `OnDisconnect`, closes the connection, and returns to the retry loop.

## The authentication engine

The Bridge does not hardcode authentication logic. When it receives a token response from the Gateway, the `strategy` field tells it exactly how to authenticate the outgoing request. The Bridge supports six strategy types:

| Strategy | How credentials are applied |
|---|---|
| `oauth2` | `Authorization: Bearer <token>` header |
| `basic_auth` | `Authorization: Basic <base64>` header |
| `header` | Custom header name with the key value |
| `query_param` | Key injected into the URL query string |
| `hmac_payload` | Request body signed with HMAC and placed in a header |
| `aws_sigv4` | Full AWS Signature Version 4 request signing |

This means the Bridge can authenticate against any provider type without code changes. You register the provider, the Broker stores the strategy, and the Bridge reads and applies it at runtime.

## WebSocket usage

```go
authClient := oauthsdk.New("http://nexus-gateway.example.com")

agentLabels := map[string]string{"agent_id": "my-agent"}
b := bridge.NewStandard(authClient, agentLabels)

err := b.MaintainWebSocket(ctx, connectionID, endpointURL, &myHandler{})
```

## gRPC usage

For gRPC, use `MaintainGRPCConnection`. You provide a connection ID, a target address, and a callback function that receives an authenticated `*grpc.ClientConn`. The Bridge injects strategy-appropriate credentials as gRPC metadata on every connection and re-authentication.

```go
runLogic := func(ctx context.Context, conn *grpc.ClientConn) error {
    client := grpc_health_v1.NewHealthClient(conn)
    resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "my-service"})
    if err != nil {
        return fmt.Errorf("health check failed: %w", err)
    }
    fmt.Printf("Health status: %s\n", resp.Status)
    return nil
}

err := b.MaintainGRPCConnection(ctx, connectionID, target, runLogic,
    grpc.WithTransportCredentials(tlsCreds),
)
```

Returning an error from the callback triggers a reconnect with backoff. Returning `nil` triggers a clean reconnect after the backoff interval.

## The Handler interface

Implement `Handler` to receive WebSocket connection events:

```go
type Handler interface {
    OnConnect(send func([]byte) error)
    OnMessage(message []byte)
    OnDisconnect(err error)
}
```

`OnConnect` gives you a thread-safe `send` function for writing messages. `OnMessage` is called for every inbound frame. `OnDisconnect` tells you why the connection closed.

## Reconnection

The outer retry loop applies exponential backoff with jitter between reconnect attempts. Jitter prevents thundering herd when many agents reconnect after a Gateway restart.

| Error type | Behaviour |
|---|---|
| Transient (network failure, abnormal close) | Retry with backoff |
| `PermanentError` (connection does not exist) | Stop immediately |
| `ErrInteractionRequired` (connection in `attention` state) | Stop immediately; surface to user |

Initial token fetch failures are always permanent. If `GetToken` fails on startup, the Bridge does not retry.

## Observability

### Prometheus metrics

The `NewStandard` constructor enables production-ready structured logging (JSON to stdout) and Prometheus metrics automatically. The `agentLabels` map is applied as constant labels to all metrics, allowing you to filter by agent in your observability stack.

| Metric | Type | Description |
|---|---|---|
| `nexus_bridge_connections_total` | Counter | Connections established |
| `nexus_bridge_disconnections_total` | Counter | Connections closed |
| `nexus_bridge_connection_status` | Gauge | `1` when connected, `0` when not |
| `nexus_bridge_token_refreshes_total` | Counter | In-place token refreshes completed |

Expose the metrics endpoint:

```go
http.Handle("/metrics", telemetry.Handler())
go http.ListenAndServe(":9090", nil)
```

### Custom Logger and Metrics

If you need your own logging or metrics implementation, use the `New` constructor with `With...` options:

```go
b := bridge.New(
    authClient,
    bridge.WithLogger(myCustomLogger),
    bridge.WithMetrics(myCustomMetrics),
    bridge.WithRetryPolicy(bridge.RetryPolicy{
        MinBackoff: 1 * time.Second,
        MaxBackoff: 60 * time.Second,
        Jitter:     500 * time.Millisecond,
    }),
)
```

The Logger and Metrics interfaces:

```go
type Logger interface {
    Info(msg string, keysAndValues ...interface{})
    Error(err error, msg string, keysAndValues ...interface{})
}

type Metrics interface {
    IncConnections()
    IncDisconnects()
    IncTokenRefreshes()
    SetConnectionStatus(status float64)
}
```

## Bridge vs SDK

Use the **Bridge** when your agent holds a persistent WebSocket or gRPC connection and you want automatic lifecycle management.

Use an **SDK** directly when your agent makes discrete HTTP calls and you want to fetch credentials explicitly per call, or when your agent is written in TypeScript or Python.
