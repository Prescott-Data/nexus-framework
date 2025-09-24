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

## Release (v0.1.0)
1) Confirm `module` path in `oauth-sdk/go.mod` is the final public path (e.g., `github.com/your-org/oauth-framework/oauth-sdk`).
2) Commit and tag:
```bash
git add oauth-sdk openapi.yaml INTEGRATIONS.md README.md
git commit -m "SDK: add Go client v0.1.0 with retries and example"
git tag v0.1.0
```
3) Push tags via Bitbucket Pipelines if used, or directly:
```bash
git push origin main --tags
```
4) Consumers add:
```bash
go get github.com/your-org/oauth-framework/oauth-sdk@v0.1.0
```
