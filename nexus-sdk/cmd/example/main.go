package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	oauthsdk "bitbucket.org/dromos/nexus-framework/nexus-sdk"
)

func main() {
	var gateway string
	var userID string
	var provider string
	var scopes string
	var returnURL string
	var connectionID string
	var noWait bool
	var noToken bool
	var useExisting bool
	var broker string
	var brokerKey string
	var retries int
	var retryMinMs int
	var retryMaxMs int
	var retry429 bool
	var enableLog bool
	flag.StringVar(&gateway, "gateway", "http://localhost:8090", "Gateway base URL")
	flag.StringVar(&userID, "user", "ws-123", "User or workspace ID")
	flag.StringVar(&provider, "provider", "Google", "Provider name")
	flag.StringVar(&scopes, "scopes", "openid,email,profile", "Comma-separated scopes")
	flag.StringVar(&returnURL, "return", "http://localhost:3000/oauth/return", "Return URL")
	flag.StringVar(&connectionID, "connection", "", "Existing connection_id to reuse (skip request-connection)")
	flag.BoolVar(&useExisting, "use-existing", false, "Use existing connection_id and skip request-connection")
	flag.BoolVar(&noWait, "no-wait", false, "Do not wait for active status")
	flag.BoolVar(&noToken, "no-token", false, "Do not fetch token")
	flag.StringVar(&broker, "broker", "", "Broker base URL (optional, for refresh)")
	flag.StringVar(&brokerKey, "broker-key", "", "Broker API key (optional, for refresh)")
	flag.IntVar(&retries, "retries", 0, "Number of retries for HTTP calls")
	flag.IntVar(&retryMinMs, "retry-min-ms", 200, "Minimum backoff in ms")
	flag.IntVar(&retryMaxMs, "retry-max-ms", 2000, "Maximum backoff in ms")
	flag.BoolVar(&retry429, "retry-429", false, "Retry on 429 status code as well")
	flag.BoolVar(&enableLog, "log", false, "Enable simple logging")
	flag.Parse()

	var opts []oauthsdk.Option
	if broker != "" {
		opts = append(opts, oauthsdk.WithBroker(broker, brokerKey))
	}
	if retries > 0 || retry429 {
		opts = append(opts, oauthsdk.WithRetry(oauthsdk.RetryPolicy{
			Retries:    retries,
			MinDelay:   time.Duration(retryMinMs) * time.Millisecond,
			MaxDelay:   time.Duration(retryMaxMs) * time.Millisecond,
			RetryOn429: retry429,
		}))
	}
	if enableLog {
		opts = append(opts, oauthsdk.WithLogger(stdLogger{}))
	}
	client := oauthsdk.New(gateway, opts...)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var id string
	if useExisting && connectionID != "" {
		id = connectionID
		fmt.Println("Using existing connection_id:", id)
	} else {
		in := oauthsdk.RequestConnectionInput{
			UserID:       userID,
			ProviderName: provider,
			Scopes:       splitCSV(scopes),
			ReturnURL:    returnURL,
		}
		rc, err := client.RequestConnection(ctx, in)
		if err != nil {
			log.Fatalf("request-connection: %v", err)
		}
		fmt.Println("Auth URL:", rc.AuthURL)
		fmt.Println("Connection ID:", rc.ConnectionID)
		id = rc.ConnectionID
	}

	if !noWait {
		status, err := client.WaitForActive(ctx, id, 2*time.Second)
		if err != nil {
			log.Fatalf("wait-for-active: %v", err)
		}
		fmt.Println("Status:", status)
	}

	if !noToken {
		tok, err := client.GetToken(ctx, id)
		if err != nil {
			log.Fatalf("get-token: %v", err)
		}
		fmt.Println("Access token length:", len(tok.AccessToken))
	}
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

type stdLogger struct{}

func (stdLogger) Infof(f string, a ...any)  { log.Printf(f, a...) }
func (stdLogger) Errorf(f string, a ...any) { log.Printf(f, a...) }
