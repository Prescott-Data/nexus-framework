package bridge

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-bridge/pkg/auth"
	"github.com/Prescott-Data/nexus-framework/nexus-bridge/telemetry"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
)

// Bridge manages persistent connections.
type Bridge struct {
	oauthClient      auth.TokenProvider
	logger           Logger
	retryPolicy      RetryPolicy
	metrics          Metrics
	refreshBuffer    time.Duration
	dialer           *websocket.Dialer
	messageSizeLimit int64
	writeTimeout     time.Duration
	pingInterval     time.Duration
}

// New creates a new Bridge with optional configurations.
func New(oauthClient auth.TokenProvider, opts ...Option) *Bridge {
	// Define default values
	bridge := &Bridge{
		oauthClient: oauthClient,
		logger:      &nopLogger{},
		metrics:     &nopMetrics{},
		retryPolicy: RetryPolicy{
			MinBackoff: 2 * time.Second,
			MaxBackoff: 30 * time.Second,
			Jitter:     1 * time.Second,
		},
		refreshBuffer:    5 * time.Minute,
		dialer:           websocket.DefaultDialer,
		messageSizeLimit: 65536, // 64KB
		writeTimeout:     10 * time.Second,
		pingInterval:     30 * time.Second,
	}

	// Apply all the functional options provided by the user
	for _, opt := range opts {
		opt(bridge)
	}

	return bridge
}

// NewStandard creates a new Bridge with production-ready defaults:
// - Structured JSON logging (Slog) to Stdout
// - Prometheus metrics registered to the default registry
func NewStandard(oauthClient auth.TokenProvider, agentLabels map[string]string, opts ...Option) *Bridge {
	// Prepend telemetry options so user can still override them if needed (though unlikely)
	defaultOpts := []Option{
		WithLogger(telemetry.NewLogger()),
		WithMetrics(telemetry.NewMetrics(nil, agentLabels)), // nil = use default registry
	}
	// Combine defaults + user opts
	finalOpts := append(defaultOpts, opts...)
	return New(oauthClient, finalOpts...)
}

// MaintainWebSocket is the main entry point. It runs a loop that attempts
// to establish and manage a connection, with a backoff policy for retries.
func (b *Bridge) MaintainWebSocket(ctx context.Context, connectionID string, endpointURL string, handler Handler) error {
	for {
		err := b.manageConnection(ctx, connectionID, endpointURL, handler)
		if err != nil {
			var permanentErr *PermanentError
			if errors.As(err, &permanentErr) {
				b.logger.Error(err, "Permanent error; will not retry", "connectionID", connectionID)
				b.metrics.SetConnectionStatus(0)
				return err // Stop the loop and return the permanent error
			}
			b.logger.Error(err, "Connection manager exited with recoverable error", "connectionID", connectionID)
		}

		select {
		case <-ctx.Done():
			b.logger.Info("Context cancelled; shutting down bridge", "connectionID", connectionID)
			b.metrics.SetConnectionStatus(0)
			return ctx.Err()
		default:
			// Connection dropped for a recoverable reason, wait and retry.
			backoff := b.calculateBackoff()
			b.logger.Info("Reconnecting", "connectionID", connectionID, "after", backoff)
			time.Sleep(backoff)
		}
	}
}

// MaintainGRPCConnection manages a persistent gRPC connection with exponential
// backoff and context-aware retry. The run callback receives each established
// connection; its return value determines whether to retry, stop, or exit cleanly.
//
// Terminal conditions (no retry):
//   - run returns nil (clean exit)
//   - run returns ErrInteractionRequired (user must re-authenticate)
//   - run returns a *PermanentError
//   - context is cancelled
func (b *Bridge) MaintainGRPCConnection(
	ctx context.Context,
	connectionID string,
	target string,
	run func(ctx context.Context, conn *grpc.ClientConn) error,
	opts ...grpc.DialOption,
) error {
	backoff := b.retryPolicy.MinBackoff
	attempt := 0

	for {
		if attempt > 0 {
			wait := b.applyJitter(backoff)
			b.logger.Info("Reconnecting gRPC", "target", target, "attempt", attempt, "after", wait)
			select {
			case <-ctx.Done():
				b.logger.Info("Context cancelled during backoff; stopping gRPC bridge", "connectionID", connectionID)
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		attempt++

		creds := NewBridgeCredentials(b.oauthClient, connectionID, b.refreshBuffer, b.logger)
		dialOpts := append(opts, grpc.WithPerRPCCredentials(creds))

		b.logger.Info("Dialing gRPC target", "target", target, "attempt", attempt)
		conn, err := grpc.NewClient(target, dialOpts...)
		if err != nil {
			b.logger.Error(err, "Failed to dial gRPC target", "target", target, "attempt", attempt)
			backoff = b.growBackoff(backoff)
			continue
		}

		b.metrics.IncConnections()
		b.metrics.SetConnectionStatus(1)
		b.logger.Info("gRPC connection established", "target", target)

		err = run(ctx, conn)

		conn.Close()
		b.metrics.SetConnectionStatus(0)
		b.metrics.IncDisconnects()

		if err == nil {
			b.logger.Info("gRPC run loop exited cleanly", "connectionID", connectionID)
			return nil
		}

		if errors.Is(err, ErrInteractionRequired) {
			b.logger.Error(err, "Interaction required; stopping gRPC retry", "connectionID", connectionID)
			return err
		}

		var permanentErr *PermanentError
		if errors.As(err, &permanentErr) {
			b.logger.Error(err, "Permanent error in gRPC run loop; stopping", "connectionID", connectionID)
			return err
		}

		if ctx.Err() != nil {
			b.logger.Info("Context cancelled; shutting down gRPC bridge", "connectionID", connectionID)
			return ctx.Err()
		}

		b.logger.Error(err, "gRPC run loop exited with error; will retry", "connectionID", connectionID, "attempt", attempt)
		backoff = b.growBackoff(backoff)
	}
}

// manageConnection handles a single connection lifecycle: get token, connect, and operate.
func (b *Bridge) manageConnection(ctx context.Context, connectionID string, endpointURL string, handler Handler) error {
	// Step 1: Get an initial token.
	token, err := b.oauthClient.GetToken(ctx, connectionID)
	if err != nil {
		// Any error during the initial token acquisition is considered permanent.
		return NewPermanentError(fmt.Errorf("failed to get initial token: %w", err))
	}
	b.logger.Info("Successfully obtained initial token", "connectionID", connectionID)

	// Step 2: Establish the WebSocket connection.
	// We create a dummy request to let the auth package inject the credentials.
	req, err := http.NewRequest("GET", endpointURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for auth injection: %w", err)
	}

	// Apply the authentication strategy.
	if err := auth.ApplyAuthentication(req, token.Strategy, token.Credentials); err != nil {
		return NewPermanentError(fmt.Errorf("failed to apply authentication strategy: %w", err))
	}

	// Dial uses the headers and the potentially modified URL (for query params).
	conn, _, err := b.dialer.Dial(req.URL.String(), req.Header)
	if err != nil {
		// WebSocket dialing errors are typically recoverable, so we don't wrap this.
		return fmt.Errorf("failed to establish WebSocket connection: %w", err)
	}
	defer conn.Close()

	conn.SetReadLimit(b.messageSizeLimit)
	conn.SetPongHandler(func(string) error {
		// Extend the read deadline upon receiving a pong.
		conn.SetReadDeadline(time.Now().Add(b.pingInterval + b.writeTimeout))
		return nil
	})

	b.metrics.IncConnections()
	b.metrics.SetConnectionStatus(1)
	b.logger.Info("Successfully established WebSocket connection", "connectionID", connectionID, "endpoint", endpointURL)

	// --- Concurrency and Shutdown Management ---
	done := make(chan struct{})       // Channel to signal shutdown to goroutines
	writeChan := make(chan []byte, 1) // Channel for thread-safe writes

	// Step 3: Call OnConnect, providing a thread-safe send function.
	sendFunc := func(message []byte) error {
		select {
		case writeChan <- message:
			return nil
		case <-done:
			return fmt.Errorf("connection is closed")
		}
	}
	handler.OnConnect(sendFunc)

	// Step 4.1: Start the "read pump" goroutine.
	readErrChan := make(chan error, 1)
	go func() {
		defer close(readErrChan)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				// Check if the error is a permanent close code.
				var closeErr *websocket.CloseError
				if errors.As(err, &closeErr) {
					if permanentCloseCodes[closeErr.Code] {
						readErrChan <- NewPermanentError(err)
						return
					}
				}
				// For other unexpected close errors, treat as recoverable.
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					readErrChan <- err
				}
				return
			}
			handler.OnMessage(message)
		}
	}()

	// Step 4.2: Start the "write pump" and "ping" goroutine for thread-safe writes and health checks.
	go func() {
		pingTicker := time.NewTicker(b.pingInterval)
		defer pingTicker.Stop()

		for {
			select {
			case message := <-writeChan:
				conn.SetWriteDeadline(time.Now().Add(b.writeTimeout))
				if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
					b.logger.Error(err, "Error writing to WebSocket", "connectionID", connectionID)
				}
			case <-pingTicker.C:
				conn.SetWriteDeadline(time.Now().Add(b.writeTimeout))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					b.logger.Error(err, "Error sending ping", "connectionID", connectionID)
					return // Assume connection is dead if ping fails.
				}
			case <-done:
				return
			}
		}
	}()

	// Step 5: Start the event loop for the active connection.
	refreshResultChan := make(chan *auth.Token, 1)
	refreshErrChan := make(chan error, 1)
	refreshing := false
	var timer *time.Timer

	for {
		var refreshTimerC <-chan time.Time
		b.logger.Info("Event loop start", "refreshing", refreshing)

		if !refreshing {
			expiresIn := time.Until(time.Unix(token.ExpiresAt, 0))
			refreshIn := expiresIn - b.refreshBuffer
			b.logger.Info("Calculated token lifetime", "expiresIn", expiresIn.String(), "refreshIn", refreshIn.String())

			if refreshIn <= 0 {
				b.logger.Info("Token expired or nearing expiry, forcing reconnect", "connectionID", connectionID)
				err := fmt.Errorf("token refresh required")
				close(done)
				b.metrics.IncDisconnects()
				b.metrics.SetConnectionStatus(0)
				handler.OnDisconnect(err)
				return err
			}
			// Only set the timer if we are not already refreshing.
			timer = time.NewTimer(refreshIn)
			refreshTimerC = timer.C
		}

		select {
		case <-ctx.Done():
			b.logger.Info("Select case: context done")
			if timer != nil {
				timer.Stop()
			}
			close(done)
			return ctx.Err()

		case err, ok := <-readErrChan:
			if !ok {
				err = fmt.Errorf("connection closed")
			}
			b.logger.Info("Select case: read error")
			if timer != nil {
				timer.Stop()
			}
			close(done)
			b.metrics.IncDisconnects()
			b.metrics.SetConnectionStatus(0)
			handler.OnDisconnect(err)
			return err

		case <-refreshTimerC: // This case is disabled if refreshTimerC is nil
			b.logger.Info("Select case: refresh timer fired")
			refreshing = true
			b.metrics.IncTokenRefreshes()
			b.logger.Info("Starting background token refresh", "connectionID", connectionID)
			go func() {
				refreshedToken, refreshErr := b.oauthClient.RefreshConnection(ctx, connectionID)
				if refreshErr != nil {
					refreshErrChan <- refreshErr
				} else {
					refreshResultChan <- refreshedToken
				}
			}()

		case refreshedToken := <-refreshResultChan:
			b.logger.Info("Select case: refresh result received")
			refreshing = false
			b.logger.Info("Successfully refreshed token in-place", "connectionID", connectionID)
			token = refreshedToken

		case refreshErr := <-refreshErrChan:
			b.logger.Info("Select case: refresh error received")
			refreshing = false
			b.logger.Error(refreshErr, "Failed to refresh token in-place; will allow connection to drop on expiry", "connectionID", connectionID)
		}
	}
}

// growBackoff doubles the current backoff, capping at MaxBackoff.
func (b *Bridge) growBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > b.retryPolicy.MaxBackoff || next <= 0 {
		return b.retryPolicy.MaxBackoff
	}
	return next
}

// applyJitter adds random jitter to a duration to prevent thundering herd
// when multiple agents reconnect simultaneously after a gateway restart.
func (b *Bridge) applyJitter(d time.Duration) time.Duration {
	if b.retryPolicy.Jitter <= 0 {
		return d
	}
	return d + time.Duration(rand.Int63n(int64(b.retryPolicy.Jitter)))
}

// calculateBackoff returns a flat backoff with jitter (used by MaintainWebSocket).
func (b *Bridge) calculateBackoff() time.Duration {
	backoff := b.retryPolicy.MinBackoff + time.Duration(rand.Int63n(int64(b.retryPolicy.Jitter)))
	if backoff > b.retryPolicy.MaxBackoff {
		return b.retryPolicy.MaxBackoff
	}
	return backoff
}
