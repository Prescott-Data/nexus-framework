# Bridge: The Universal Go Connector

## Overview

The Bridge is a client-side Go library for creating and maintaining persistent, observable, and authenticated connections using **WebSocket** or **gRPC**.

It is designed to be embedded within an "agent" or service that needs a long-lived, resilient connection to an external system. By handling the complex logic of authentication, token refreshing, and reconnection with exponential backoff, the Bridge allows you to focus on your business logic.

## Key Features

- **Multi-Transport:** Out-of-the-box support for both **WebSocket** and **gRPC** persistent connections.
- **Generic Authentication Engine:** No more hardcoded logic. The Bridge authenticates using a dynamic strategy ("oauth2", "basic_auth", "header", "query_param", "hmac_payload", "aws_sigv4") provided by your backend, making it a universal connector.
- **Built-in Observability:** The `NewStandard` constructor automatically enables production-ready structured logging (JSON to stdout) and Prometheus metrics, ready for cloud-native collection.
- **Persistent Connections:** Automatically reconnects with a configurable exponential backoff and jitter strategy if a connection drops.
- **Proactive Credential Refresh:** Intelligently refreshes authentication credentials *before* they expire to ensure connections remain valid without interruption.
- **Robust Error Handling:** Distinguishes between transient, recoverable errors (which trigger a retry) and permanent errors (which cause it to stop).

## Standard Usage

For most applications, use the `NewStandard` constructor to get an observable, production-ready Bridge instance.

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	
	"dromos.io/nexus-bridge"
	"dromos.io/nexus-bridge/telemetry"
	"bitbucket.org/dromos/nexus-framework/nexus-sdk" // The client for your auth backend
)

// 1. Define your WebSocket handler
type myWsHandler struct{}
func (h *myWsHandler) OnConnect(send func(message []byte) error) { fmt.Println("Connected!") }
func (h *myWsHandler) OnMessage(message []byte)                   { fmt.Printf("Msg: %s\n", message) }
func (h *myWsHandler) OnDisconnect(err error)                     { fmt.Printf("Disconnected: %v\n", err) }

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Create a client for your auth backend
	// This must implement the bridge.OAuthClient interface
	authClient := oauthsdk.New("http://nexus-gateway.example.com")

	// 3. Instantiate the Bridge with production-ready telemetry
	// agentLabels are applied as const_labels to all Prometheus metrics
	agentLabels := map[string]string{"agent_id": "my-stable-id"}
	b := bridge.NewStandard(authClient, agentLabels)

	// 4. Run the Bridge in a goroutine
	go func() {
		connectionID := "your-connection-id"
		endpointURL := "wss://external.system.com/ws"
		// The Bridge handles all auth and reconnect logic internally
		err := b.MaintainWebSocket(ctx, connectionID, endpointURL, &myWsHandler{})
		if err != nil {
			fmt.Printf("Bridge exited with permanent error: %v\n", err)
		}
	}()
	
	// 5. Expose the Prometheus metrics endpoint
	http.Handle("/metrics", telemetry.Handler())
	fmt.Println("Serving metrics on :9090")
	go http.ListenAndServe(":9090", nil)

	// ... your application logic ...
	select {}
}
```

## gRPC Usage

The Bridge can also manage a persistent gRPC connection, handling all authentication and reconnection logic for you.

```go
import (
	"context"
	"fmt"
	
	"dromos.io/nexus-bridge"
	"bitbucket.org/dromos/nexus-framework/nexus-sdk"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1" // Example service
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	authClient := oauthsdk.New("http://nexus-gateway.example.com")
	// agentLabels are applied as const_labels to all Prometheus metrics
	agentLabels := map[string]string{"agent_id": "my-stable-id"}
	b := bridge.NewStandard(authClient, agentLabels)

	// This function contains your business logic. The Bridge will call it
	// every time a fresh, authenticated connection is established.
	runLogic := func(ctx context.Context, conn *grpc.ClientConn) error {
		client := grpc_health_v1.NewHealthClient(conn)
		
		// Make your RPC calls
		resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "my-service"})
		if err != nil {
			// Returning an error will cause the Bridge to reconnect
			return fmt.Errorf("health check failed: %w", err)
		}
		
		fmt.Printf("Health status: %s\n", resp.Status)
		
		// Keep the connection alive for a while, or do streaming work
		time.Sleep(30 * time.Second)
		
		// Returning nil will also trigger a clean reconnect after backoff
		return nil
	}
	
	go func() {
		connectionID := "your-grpc-connection-id"
		target := "my-grpc-service.example.com:443"

		// Example with TLS
		tlsCreds := credentials.NewTLS(&tls.Config{ /* ... */ })
		
		err := b.MaintainGRPCConnection(
			ctx, 
			connectionID, 
			target, 
			runLogic, 
			grpc.WithTransportCredentials(tlsCreds),
		)
		if err != nil {
			fmt.Printf("Bridge exited with permanent error: %v", err)
		}
	}()

	select {}
}
```

## Advanced Configuration

If you need to provide your own logging or metrics implementation, use the `New` constructor with `With...` options.

```go
	// Example using a custom logger and retry policy
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

## Interfaces for Extension

You can integrate your own logging and metrics systems by implementing these interfaces.

### Logger Interface

```go
// Logger is an interface that allows for plugging in custom structured loggers.
type Logger interface {
	Info(msg string, keysAndValues ...interface{})
	Error(err error, msg string, keysAndValues ...interface{})
}
```

### Metrics Interface

```go
// Metrics is an interface that allows for plugging in custom metrics collectors.
type Metrics interface {
	IncConnections()
	IncDisconnects()
	IncTokenRefreshes()
	SetConnectionStatus(status float64)
}
```