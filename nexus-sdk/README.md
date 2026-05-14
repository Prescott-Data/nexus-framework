# Nexus SDK — Go

The official Go client for the [Nexus Framework](https://github.com/Prescott-Data/nexus-framework) — a provider-agnostic OAuth 2.0 / OIDC integration layer for autonomous agents and MCP servers.

## Install

```bash
go get github.com/Prescott-Data/nexus-framework/nexus-sdk@latest
```

## Quick Start

### Standard App Flow

```go
import (
    "context"
    "time"
    oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

client := oauthsdk.New("https://nexus-gateway.example.com")

// 1. Initiate OAuth consent
conn, err := client.RequestConnection(ctx, oauthsdk.RequestConnectionInput{
    UserID:       "user-123",
    ProviderName: "github",
    Scopes:       []string{"repo", "read:user"},
    ReturnURL:    "https://myapp.com/callback",
})
// → Redirect user to conn.AuthURL

// 2. Poll until user completes consent
status, err := client.WaitForActive(ctx, conn.ConnectionID, 1500*time.Millisecond)

// 3. Fetch token
token, err := client.GetToken(ctx, conn.ConnectionID)
fmt.Println(token.AccessToken)
```

### MCP Server Flow

Use `AuthenticatedHTTPClient` to automatically resolve and inject tokens into every outbound HTTP request — the Go equivalent of TypeScript's `createFetcher`.

```go
import (
    "time"
    oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

client := oauthsdk.New("https://nexus-gateway.example.com")
cache  := oauthsdk.NewTokenCache(30 * time.Second)

// Create a standard *http.Client with automatic token injection.
// workspaceID identifies the tenant; provider is "github", "notion", etc.
httpClient := client.AuthenticatedHTTPClient(cache, "workspace-123", "github")

// All requests are automatically authorized — no manual token handling.
resp, err := httpClient.Get("https://api.github.com/user/repos")
```

## Configuration

```go
client := oauthsdk.New(
    "https://nexus-gateway.example.com",

    // Structured logger (optional)
    oauthsdk.WithLogger(myLogger),

    // Retry policy with exponential backoff + jitter (optional)
    oauthsdk.WithRetry(oauthsdk.RetryPolicy{
        Retries:    3,
        MinDelay:   200 * time.Millisecond,
        MaxDelay:   2 * time.Second,
        RetryOn429: true,
    }),
)
```

### Logger Interface

```go
type Logger interface {
    Infof(format string, args ...any)
    Errorf(format string, args ...any)
}
```

## API Reference

### Gateway Methods

| Method | Description |
|---|---|
| `RequestConnection(ctx, input)` | Initiate an OAuth consent flow. Returns the `authUrl` and `connectionId`. |
| `CheckConnection(ctx, connectionID)` | Poll the connection status (`"active"`, `"pending"`, `"failed"`). |
| `GetToken(ctx, connectionID)` | Retrieve the current token for a connection. |
| `RefreshConnection(ctx, connectionID)` | Force a token refresh via the gateway. |
| `WaitForActive(ctx, connectionID, interval)` | Poll `CheckConnection` until terminal status or context expiry. |

### MCP / Workspace Methods

| Method | Description |
|---|---|
| `ResolveToken(ctx, workspaceID, provider)` | Resolve a fresh token by workspace + provider name via `GET /v1/resolve`. |
| `GetCachedToken(ctx, cache, workspaceID, provider)` | Resolve a token from `TokenCache`, or fetch fresh. |
| `NewAuthenticatedTransport(cache, workspaceID, provider, base)` | Returns an `http.RoundTripper` that injects `Authorization` headers. |
| `AuthenticatedHTTPClient(cache, workspaceID, provider)` | Returns a ready-to-use `*http.Client` with token injection. |

### TokenCache

```go
// Create a cache with a 30-second safety buffer before token expiry
cache := oauthsdk.NewTokenCache(30 * time.Second)

cache.Get(workspaceID, provider)           // *CachedToken or nil
cache.Set(workspaceID, provider, token)    // store a token
cache.Delete(workspaceID, provider)        // evict manually
```

## Error Handling

All methods return standard Go `error` values. Gateway-level errors are returned as `ErrorEnvelope`:

```go
token, err := client.GetToken(ctx, connectionID)
if err != nil {
    var envelope oauthsdk.ErrorEnvelope
    if errors.As(err, &envelope) {
        fmt.Println(envelope.Code)    // e.g., "connection_not_found"
        fmt.Println(envelope.Message) // human-readable description
    }
}
```

## Testing

```bash
# Unit tests (no network required)
go test ./...

# Integration smoke test against the live gateway
NEXUS_GATEWAY_URL=https://your-gateway.example.com go run ./cmd/smoke-mcp/
```

## Notes

- The SDK **never logs token bodies**.
- All `RefreshConnection` calls go through the Gateway proxy — the Broker is never exposed to your application.
- Token types are normalized to `Bearer` (capitalized) per RFC 6750, regardless of how the provider returns them.
