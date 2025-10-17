# Bridge Client Library

## Overview

The Bridge is a client-side Go library responsible for creating and maintaining a persistent, authenticated WebSocket connection. 

It is designed to be embedded within an "agent" or service that needs a long-lived connection to an external system. By handling the connection lifecycle within the agent itself, the Bridge decentralizes connection management. This avoids creating a central bottleneck, enhances scalability, and aligns with a lightweight, distributed architecture where each agent is responsible for its own persistent connection.

## Key Features

- **Authentication**: Automatically uses the OAuth framework to fetch and refresh access tokens for the connection.
- **Persistent Connection**: Automatically reconnects with a configurable exponential backoff and jitter strategy if the connection drops.
- **Proactive Token Refresh**: Intelligently refreshes the authentication token *before* it expires to ensure the connection remains valid without interruption.
- **Robust Error Handling**: Distinguishes between transient, recoverable errors (which trigger a retry) and permanent errors (which cause it to stop), preventing wasted resources.
- **Production-Ready**: Highly configurable with interfaces for structured logging, metrics, and custom connection dialers.

## Usage

Here is an example of how to instantiate and run the bridge.

### Basic Example

```go
package main

import (
	"context"
	"fmt"

	"dromos.io/bridge"
	"dromos.io/oauth-sdk" // Assuming the sdk provides the OAuthClient
)

// 1. Define your handler
type myHandler struct{}

func (h *myHandler) OnConnect(send func(message []byte) error) {
	fmt.Println("Bridge connected!")
	send([]byte("hello from agent"))
}

func (h *myHandler) OnMessage(message []byte) {
	fmt.Printf("Message received: %s\n", string(message))
}

func (h *myHandler) OnDisconnect(err error) {
	fmt.Printf("Bridge disconnected: %v\n", err)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Create an OAuth client from the oauth-sdk
	oauthClient := oauthsdk.New("http://localhost:8081")

	// 3. Instantiate the bridge
	bridge := bridge.New(oauthClient)

	// 4. Run the bridge in a goroutine
	connectionID := "your-connection-id"
	endpointURL := "wss://external.system.com/ws"
	go func() {
		err := bridge.MaintainWebSocket(ctx, connectionID, endpointURL, &myHandler{})
		if err != nil {
			fmt.Printf("Bridge exited with error: %v\n", err)
		}
	}()

	// ... your application logic ...
	select {}
}
```

### Advanced Configuration

The bridge can be customized using various `Option` functions.

```go
	retryPolicy := bridge.RetryPolicy{
		MinBackoff: 1 * time.Second,
		MaxBackoff: 60 * time.Second,
		Jitter:     500 * time.Millisecond,
	}

	// Example using a custom logger, metrics, and retry policy
	bridge := bridge.New(
		oauthClient,
		bridge.WithLogger(myStructuredLogger),
		bridge.WithMetrics(myPrometheusMetrics),
		bridge.WithRetryPolicy(retryPolicy),
		bridge.WithRefreshBuffer(10 * time.Minute),
	)
```

## Handler Interface

To use the bridge, you must provide an implementation of the `Handler` interface.

```go
// Handler defines the interface for handling events from a persistent connection.
type Handler interface {
	// OnConnect is called when a new connection is successfully established.
	// The `send` function can be used by the handler to send messages back
	// through the bridge.
	OnConnect(send func(message []byte) error)

	// OnMessage is called when a new message is received from the connection.
	OnMessage(message []byte)

	// OnDisconnect is called when the connection is lost. The bridge will
	// automatically attempt to reconnect unless the disconnect was caused by a
	// permanent error.
	OnDisconnect(err error)
}
```

## Configuration Options

The following options can be passed to the `New` function to customize the bridge's behavior.

- `WithLogger(logger Logger)`: Plugs in a custom structured logger. Defaults to a no-op logger.
- `WithMetrics(metrics Metrics)`: Plugs in a metrics collector. Defaults to a no-op collector.
- `WithRetryPolicy(policy RetryPolicy)`: Sets the backoff strategy for reconnections.
- `WithRefreshBuffer(d time.Duration)`: Sets how long before token expiry the bridge should attempt to refresh it. Defaults to 5 minutes.
- `WithDialer(dialer *websocket.Dialer)`: Provides a custom `websocket.Dialer`, allowing for configuration of handshake timeouts, TLS settings, etc. Defaults to `websocket.DefaultDialer`.

## Error Handling

The bridge automatically retries on transient errors (e.g., temporary network issues). However, it will exit immediately if it encounters a `PermanentError`.

This type of error is returned in two main scenarios:
1.  **Initial Token Failure**: If the bridge cannot acquire a valid token on its first attempt, it cannot proceed.
2.  **Non-Recoverable Close Code**: If the WebSocket server closes the connection with a specific, non-recoverable code (e.g., `1008 Policy Violation`), the bridge will not attempt to reconnect.

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
	IncConnections()      // Called on each successful connection.
	IncDisconnects()      // Called when a connection is lost.
	IncTokenRefreshes()   // Called when a token refresh cycle is initiated.
	SetConnectionStatus(status float64) // Use 1 for connected, 0 for disconnected.
}
```
