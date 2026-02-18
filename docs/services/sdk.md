# Nexus SDK

The **Nexus SDK** is the lightweight Go client used by your application's **Control Plane** logic to manage user connections.

## Core Features

- **Connection Management:** Start handshakes and check status.
- **Token Retrieval:** Fetch the current Strategy and Credentials.
- **Automatic Retries:** Built-in exponential backoff for Gateway calls.
- **Polling Helpers:** `WaitForActive` simplifies the "waiting for user consent" flow.

## Common Operations

### Initialize Client
```go
client := nexus.New("https://nexus-gateway.example.com")
```

### Request a Connection
```go
resp, err := client.RequestConnection(ctx, nexus.RequestConnectionInput{
    UserID:       "user-1",
    ProviderName: "google",
    Scopes:       []string{"email", "profile"},
    ReturnURL:    "https://my-app.com/callback",
})
```

### Wait for User Consent
```go
// Polls the gateway until the user completes the flow or the context expires
status, err := client.WaitForActive(ctx, connectionID, 2 * time.Second)
```

### Force a Refresh
```go
// Manually trigger a token refresh
token, err := client.RefreshConnection(ctx, connectionID)
```
