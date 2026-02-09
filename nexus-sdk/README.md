# Dromos OAuth SDK (Go)

Thin Go client for the Dromos OAuth Gateway v1 endpoints defined in `../openapi.yaml`.

Status: experimental; API surface is frozen to v1 paths.

## Usage

The SDK acts as a generic credential fetcher. The `TokenResponse` contains the specific authentication details provided by your backend.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"bitbucket.org/dromos/nexus-framework/nexus-sdk"
)

func main() {
	ctx := context.Background()
	client := oauthsdk.New("https://gateway.example.com")

	// 1) Request a connection (this part of the flow is typically for user-interactive OAuth2)
	// For service-to-service connections, you would likely have a pre-existing connection ID.
	connectionID := "pre-existing-connection-id"

	// 2) Get the credential payload for the connection
	tr, err := client.GetToken(ctx, connectionID)
	if err != nil {
		log.Fatalf("Failed to get token: %v", err)
	}
	
	// 3) Inspect the response to determine how to authenticate
	
	// The Strategy field tells you how to authenticate.
	// It's a map that can be deserialized into a struct or inspected directly.
	strategyType, _ := tr.Strategy["type"].(string)
	fmt.Printf("Authentication Strategy: %s\n", strategyType)
	
	// The Credentials field contains the secrets.
	// It's a map that can be passed to an auth engine like the Bridge.
	fmt.Printf("Credentials Map: %v\n", tr.Credentials)
	
	// For backward compatibility with simple OAuth2 flows, AccessToken is still populated.
	if strategyType == "oauth2" {
		fmt.Printf("Access Token: %s\n", tr.AccessToken)
	}
}
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
- Force Refresh:
```go
// Force a refresh of the connection credentials via the Gateway
newToken, err := client.RefreshConnection(ctx, connectionID)
```

## Notes
- The SDK never logs token bodies.
- Prefer Gateway-only flows. The `RefreshConnection` method uses the Gateway's proxy, keeping the Broker private.

## Install
```bash
go get bitbucket.org/dromos/nexus-framework/nexus-sdk@v0.1.1
```
