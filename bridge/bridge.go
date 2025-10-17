package bridge

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// MOCK for dromos-oauth-sdk
type OAuthClient interface {
	GetToken(ctx context.Context, connectionID string) (*Token, error)
	RefreshViaBroker(ctx context.Context, connectionID string) (*Token, error)
}

type Token struct {
	AccessToken string
	ExpiresAt   int64 // Unix timestamp
}

// Bridge manages persistent connections.
type Bridge struct {
	oauthClient   OAuthClient
	logger        Logger
	retryPolicy   RetryPolicy
	metrics       Metrics
	refreshBuffer time.Duration
	dialer        *websocket.Dialer
}

// New creates a new Bridge with optional configurations.
func New(oauthClient OAuthClient, opts ...Option) *Bridge {
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
		refreshBuffer: 5 * time.Minute,
		dialer:        websocket.DefaultDialer,
	}

	// Apply all the functional options provided by the user
	for _, opt := range opts {
		opt(bridge)
	}

	return bridge
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
	headers := http.Header{}
	headers.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
	conn, _, err := b.dialer.Dial(endpointURL, headers)
	if err != nil {
		// WebSocket dialing errors are typically recoverable, so we don't wrap this.
		return fmt.Errorf("failed to establish WebSocket connection: %w", err)
	}
	defer conn.Close()

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

	// Step 4.2: Start the "write pump" goroutine for thread-safe writes.
	go func() {
		for {
			select {
			case message := <-writeChan:
				if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
					b.logger.Error(err, "Error writing to WebSocket", "connectionID", connectionID)
				}
			case <-done:
				return
			}
		}
	}()

	// Step 5: Start the event loop for the active connection.
	refreshResultChan := make(chan *Token, 1)
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

		case err := <-readErrChan:
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
				refreshedToken, refreshErr := b.oauthClient.RefreshViaBroker(ctx, connectionID)
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

// NEW: Helper function for calculating backoff with jitter.
func (b *Bridge) calculateBackoff() time.Duration {
	backoff := b.retryPolicy.MinBackoff + time.Duration(rand.Int63n(int64(b.retryPolicy.Jitter)))
	if backoff > b.retryPolicy.MaxBackoff {
		return b.retryPolicy.MaxBackoff
	}
	return backoff
}
