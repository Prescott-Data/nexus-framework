---
icon: material/language-go
---

# Go SDK

The `nexus-sdk` Go package is a zero-dependency client for the Nexus Gateway. It supports both standard OAuth connection management and MCP server token injection.

## Install

```bash
go get github.com/Prescott-Data/nexus-framework/nexus-sdk@latest
```

## Initialization

```go
import oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"

// Minimal
client := oauthsdk.New("https://nexus-gateway.example.com")

// With options
client := oauthsdk.New(
    "https://nexus-gateway.example.com",
    oauthsdk.WithLogger(myLogger),
    oauthsdk.WithRetry(oauthsdk.RetryPolicy{
        Retries:    3,
        MinDelay:   200 * time.Millisecond,
        MaxDelay:   2 * time.Second,
        RetryOn429: true,
    }),
)
```

## Standard App Methods

### RequestConnection

Initiate an OAuth consent flow. Wraps `POST /v1/request-connection`.

```go
conn, err := client.RequestConnection(ctx, oauthsdk.RequestConnectionInput{
    UserID:       "user-123",
    ProviderName: "github",
    Scopes:       []string{"repo", "read:user"},
    ReturnURL:    "https://myapp.com/callback",
})
// conn.AuthURL    → redirect user here
// conn.ConnectionID → persist this
```

### CheckConnection

Poll connection status. Wraps `GET /v1/check-connection/{id}`.

```go
status, err := client.CheckConnection(ctx, connectionID)
// returns: "active" | "pending" | "failed"
```

### WaitForActive

Polls `CheckConnection` until the status is terminal or the context expires.

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()

status, err := client.WaitForActive(ctx, connectionID, 1500*time.Millisecond)
```

### GetToken

Fetch the current credential payload. Wraps `GET /v1/token/{id}`.

```go
token, err := client.GetToken(ctx, connectionID)
fmt.Println(token.AccessToken)
fmt.Println(token.Strategy) // map: {"type": "oauth2"}
```

### RefreshConnection

Force a token refresh via the Gateway proxy. Wraps `POST /v1/refresh/{id}`.

```go
newToken, err := client.RefreshConnection(ctx, connectionID)
```

## MCP / Workspace Methods

### ResolveToken

Resolve a token by workspace ID and provider name. Wraps `GET /v1/resolve`.

```go
token, err := client.ResolveToken(ctx, "workspace-123", "github")
fmt.Println(token.AccessToken)
```

### GetCachedToken

Cache-aware wrapper around `ResolveToken`. Fetches fresh only on cache miss or expiry.

```go
cache := oauthsdk.NewTokenCache(30 * time.Second)

token, err := client.GetCachedToken(ctx, cache, "workspace-123", "github")
```

### AuthenticatedHTTPClient

Returns a fully configured `*http.Client` that automatically injects `Authorization` headers into every request.

```go
cache := oauthsdk.NewTokenCache(30 * time.Second)

httpClient := client.AuthenticatedHTTPClient(cache, "workspace-123", "github")
resp, err := httpClient.Get("https://api.github.com/user/repos")
```

### NewAuthenticatedTransport

For advanced use cases — wraps an existing `http.RoundTripper` with token injection.

```go
transport := client.NewAuthenticatedTransport(cache, "workspace-123", "github", http.DefaultTransport)

httpClient := &http.Client{Transport: transport, Timeout: 30 * time.Second}
```

## TokenCache

A thread-safe, TTL-aware in-memory cache. The `buffer` is a safety margin subtracted from the token's expiry time so re-fetches happen slightly before the upstream token actually expires.

```go
cache := oauthsdk.NewTokenCache(30 * time.Second) // 30s safety buffer

cache.Get(workspaceID, provider)         // *CachedToken or nil
cache.Set(workspaceID, provider, token)  // store
cache.Delete(workspaceID, provider)      // evict
```

## Error Handling

Non-retryable gateway errors implement the `error` interface as `ErrorEnvelope`:

```go
token, err := client.GetToken(ctx, connectionID)
if err != nil {
    if e, ok := err.(oauthsdk.ErrorEnvelope); ok {
        fmt.Println(e.Code)    // "connection_not_found"
        fmt.Println(e.Message)
    }
}
```

## Logger Interface

```go
type Logger interface {
    Infof(format string, args ...any)
    Errorf(format string, args ...any)
}

// Example implementation
type stdLogger struct{}
func (stdLogger) Infof(f string, a ...any)  { log.Printf("[nexus] "+f, a...) }
func (stdLogger) Errorf(f string, a ...any) { log.Printf("[nexus:err] "+f, a...) }
```

## Security Notes

- The SDK **never logs token bodies** or credentials.
- `RefreshConnection` routes through the Gateway proxy — the Broker is never exposed.
- Token types are normalized to `Bearer` (capitalized) per RFC 6750.
