# Nexus SDK

The **Nexus SDK** is a lightweight Go client library for interacting with the **Nexus Gateway**.

It is primarily used by your application's **Control Plane** (backend) to:
1.  Initiate new connection flows (OAuth handshakes).
2.  Poll for connection status.
3.  Retrieve credentials on-demand for use by agents.

> **Note:** If you are building an Agent that needs to maintain a persistent connection (WebSocket/gRPC), check out the [Nexus Bridge](../nexus-bridge) instead. The SDK is a lower-level primitive.

## Installation

```bash
go get github.com/Prescott-Data/nexus-framework/nexus-sdk
```

## Quick Start

### 1. Initialize the Client

```go
package main

import (
    "time"
    oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

func main() {
    // Point to your public Gateway URL
    client := oauthsdk.New(
        "https://gateway.example.com",
        oauthsdk.WithTimeout(10 * time.Second),
    )
}
```

### 2. Request a Connection (OAuth Flow)

This generates the Authorization URL you send to the user.

```go
req := oauthsdk.RequestConnectionInput{
    UserID:       "user-123",
    ProviderName: "google",
    Scopes:       []string{"email", "profile"},
    ReturnURL:    "https://myapp.com/callback",
}

resp, err := client.RequestConnection(ctx, req)
if err != nil {
    panic(err)
}

fmt.Printf("Redirect User to: %s\n", resp.AuthURL)
fmt.Printf("Track Connection ID: %s\n", resp.ConnectionID)
```

### 3. Check Status

Useful for polling while the user is in the consent flow.

```go
status, err := client.CheckConnection(ctx, resp.ConnectionID)
if status == "active" {
    fmt.Println("Connection is ready!")
}
```

### 4. Retrieve Credentials

Fetches the active secrets. The Gateway automatically handles token refreshing if necessary.

```go
token, err := client.GetToken(ctx, resp.ConnectionID)
if err != nil {
    panic(err)
}

// Inspect the Strategy to know how to use the credentials
switch token.Strategy["type"] {
case "oauth2":
    fmt.Printf("Bearer Token: %s\n", token.AccessToken)
case "api_key":
    fmt.Printf("API Key: %s\n", token.Credentials["api_key"])
}
```

---

## Advanced Configuration

### Retry Policy
Configure robust retries for transient network errors.

```go
client := oauthsdk.New(
    "https://gateway.example.com",
    oauthsdk.WithRetry(oauthsdk.RetryPolicy{
        Retries:    3,
        MinDelay:   100 * time.Millisecond,
        MaxDelay:   2 * time.Second,
        RetryOn429: true,
    }),
)
```

### Custom Logger
Inject your own logger (e.g., Zap, Logrus) to debug SDK operations.

```go
type MyLogger struct{}
func (l MyLogger) Infof(format string, args ...any) { /* ... */ }
func (l MyLogger) Errorf(format string, args ...any) { /* ... */ }

client := oauthsdk.New(
    "...",
    oauthsdk.WithLogger(MyLogger{}),
)
```

### Force Refresh
Manually trigger a token refresh if your downstream calls fail with `401 Unauthorized`.

```go
newToken, err := client.RefreshConnection(ctx, connectionID)
```
