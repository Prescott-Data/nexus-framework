# The Bridge

The Bridge is a Go library you embed in your agent process. It maintains an authenticated connection for you — fetching tokens, injecting credentials into requests, and refreshing tokens before expiry — so your agent code handles only application logic.

## How it works

When you call `MaintainWebSocket` or `MaintainGRPC`, the Bridge runs a `manageConnection` loop:

1. Calls `GET /v1/token/{connection_id}` to fetch the current token and auth strategy.
2. Applies the strategy to authenticate the underlying connection (WebSocket headers or gRPC metadata).
3. Starts three goroutines: a read pump, a write pump, and a ping ticker.
4. Monitors `ExpiresAt` and fires a background `RefreshConnection` before the token expires — the connection stays open during refresh.
5. On any error, calls `OnDisconnect`, closes the connection, and returns to the retry loop.

## The Handler interface

Implement `Handler` to receive connection events:

```go
type Handler interface {
    OnConnect(send func([]byte) error)
    OnMessage(message []byte)
    OnDisconnect(err error)
}
```

`OnConnect` gives you a thread-safe `send` function. Use it to write messages. `OnMessage` is called for every inbound frame. `OnDisconnect` tells you why the connection closed.

## Reconnection

The outer retry loop applies exponential backoff with jitter between reconnect attempts. Jitter prevents thundering herd when many agents reconnect after a Gateway restart.

| Error type | Behaviour |
|---|---|
| Transient (network failure, abnormal close) | Retry with backoff |
| `PermanentError` (connection does not exist) | Stop immediately |
| `ErrInteractionRequired` (connection in `attention` state) | Stop immediately — surface to user |

Initial token fetch failures are always permanent. If `GetToken` fails on startup, the Bridge does not retry.

## Prometheus metrics

| Metric | Type | Description |
|---|---|---|
| `nexus_bridge_connections_total` | Counter | Connections established |
| `nexus_bridge_disconnections_total` | Counter | Connections closed |
| `nexus_bridge_connection_status` | Gauge | `1` when connected, `0` when not |
| `nexus_bridge_token_refreshes_total` | Counter | In-place token refreshes completed |

## Bridge vs SDK

Use the **Bridge** when your agent holds a persistent WebSocket or gRPC connection and you want automatic lifecycle management.

Use the **SDK** directly when your agent makes discrete HTTP calls and you want to fetch credentials explicitly per call.
