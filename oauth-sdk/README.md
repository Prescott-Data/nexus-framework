# Dromos OAuth SDK (Go)

Thin Go client for the Dromos OAuth Gateway v1 endpoints defined in `../openapi.yaml`.

Status: experimental; API surface is frozen to v1 paths.

## Usage

```go
ctx := context.Background()
client := oauthsdk.New("https://gateway.example.com")

// 1) Request connection
rc, err := client.RequestConnection(ctx, oauthsdk.RequestConnectionInput{
    UserID:       "workspace-123",
    ProviderName: "Google",
    Scopes:       []string{"openid", "email", "profile"},
    ReturnURL:    "https://app.example.com/oauth/return",
})
fmt.Println(rc.AuthURL, rc.ConnectionID)

// 2) Wait for active
status, _ := client.WaitForActive(ctx, rc.ConnectionID, 0)

// 3) Get token
tr, _ := client.GetToken(ctx, rc.ConnectionID)
```

### Options
- Logger hook:
```go
type stdLogger struct{}
func (stdLogger) Infof(f string, a ...any)  { log.Printf(f, a...) }
func (stdLogger) Errorf(f string, a ...any) { log.Printf(f, a...) }

client := oauthsdk.New(
  "https://gateway.example.com",
  oauthsdk.WithLogger(stdLogger{}),
)
```
- Retry policy:
```go
client := oauthsdk.New(
  "https://gateway.example.com",
  oauthsdk.WithRetry(oauthsdk.RetryPolicy{Retries: 3, MinDelay: 200*time.Millisecond, MaxDelay: 2*time.Second, RetryOn429: true}),
)
```
- Broker refresh (temporary):
```go
client := oauthsdk.New(
  "https://gateway.example.com",
  oauthsdk.WithBroker("https://broker.internal", os.Getenv("BROKER_API_KEY")),
)
newToken, err := client.RefreshViaBroker(ctx, connectionID)
```

## Notes
- The SDK never logs token bodies.
- Keep Broker private; allowlist callers and require API key for refresh.
- Prefer Gateway-only flows; `RefreshViaBroker` is temporary until a Gateway refresh proxy exists.

## Install
```bash
go get bitbucket.org/dromos/oauth-framework/oauth-sdk@v0.1.1
```
