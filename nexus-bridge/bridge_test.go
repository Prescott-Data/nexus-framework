package bridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-bridge/pkg/auth"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// --- Mocks ---

// mockTokenProvider is a mock implementation of auth.TokenProvider for testing.
type mockTokenProvider struct {
	getTokenFunc          func(ctx context.Context, connectionID string) (*auth.Token, error)
	refreshConnectionFunc func(ctx context.Context, connectionID string) (*auth.Token, error)
}

func (m *mockTokenProvider) GetToken(ctx context.Context, connectionID string) (*auth.Token, error) {
	return m.getTokenFunc(ctx, connectionID)
}

func (m *mockTokenProvider) RefreshConnection(ctx context.Context, connectionID string) (*auth.Token, error) {
	if m.refreshConnectionFunc != nil {
		return m.refreshConnectionFunc(ctx, connectionID)
	}
	return nil, errors.New("not implemented")
}

// mockHandler is a mock implementation of the Handler interface for testing.
type mockHandler struct {
	mu           sync.Mutex
	onConnect    func(send func(message []byte) error)
	onMessage    func(message []byte)
	onDisconnect func(err error)
}

func (h *mockHandler) OnConnect(send func(message []byte) error) {
	if h.onConnect != nil {
		h.onConnect(send)
	}
}

func (h *mockHandler) OnMessage(message []byte) {
	if h.onMessage != nil {
		h.onMessage(message)
	}
}

func (h *mockHandler) OnDisconnect(err error) {
	if h.onDisconnect != nil {
		h.onDisconnect(err)
	}
}

// mockMetrics is a mock implementation of the Metrics interface for testing.
type mockMetrics struct {
	connections      int32
	disconnects      int32
	tokenRefreshes   int32
	connectionStatus atomic.Value
}

func (m *mockMetrics) IncConnections()                    { atomic.AddInt32(&m.connections, 1) }
func (m *mockMetrics) IncDisconnects()                    { atomic.AddInt32(&m.disconnects, 1) }
func (m *mockMetrics) IncTokenRefreshes()                 { atomic.AddInt32(&m.tokenRefreshes, 1) }
func (m *mockMetrics) SetConnectionStatus(status float64) { m.connectionStatus.Store(status) }

// testLogger is a mock implementation of the Logger interface for testing.
type testLogger struct {
	t *testing.T
}

func (l *testLogger) Info(msg string, keysAndValues ...interface{}) {
	l.t.Logf("INFO: %s %v", msg, keysAndValues)
}

func (l *testLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.t.Logf("ERROR: %s %v err: %v", msg, keysAndValues, err)
}

var upgrader = websocket.Upgrader{}

// --- Tests ---

func TestBridge_PermanentTokenError(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			return nil, errors.New("invalid credentials")
		},
	}

	metrics := &mockMetrics{}
	bridge := New(authClient, WithMetrics(metrics))
	handler := &mockHandler{}

	err := bridge.MaintainWebSocket(context.Background(), "conn-123", "ws://localhost", handler)

	var permanentErr *PermanentError
	if !errors.As(err, &permanentErr) {
		t.Fatalf("Expected a PermanentError, but got: %v", err)
	}
	if metrics.connectionStatus.Load() != 0.0 {
		t.Errorf("Expected connection status to be 0, but got %v", metrics.connectionStatus.Load())
	}
}

func TestBridge_PermanentCloseCode(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "test-token"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		// Close immediately with a policy violation code.
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "auth failed"))
		conn.Close()
	}))
	defer server.Close()

	metrics := &mockMetrics{}
	retryPolicy := RetryPolicy{MinBackoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond, Jitter: 5 * time.Millisecond}
	bridge := New(authClient, WithMetrics(metrics), WithRetryPolicy(retryPolicy))
	handler := &mockHandler{}

	err := bridge.MaintainWebSocket(context.Background(), "conn-123", "ws"+server.URL[4:], handler)

	var permanentErr *PermanentError
	if !errors.As(err, &permanentErr) {
		t.Fatalf("Expected a PermanentError for a permanent close code, but got: %v", err)
	}
	if atomic.LoadInt32(&metrics.connections) != 1 {
		t.Errorf("Expected 1 connection attempt, got %d", metrics.connections)
	}
	if atomic.LoadInt32(&metrics.disconnects) != 1 {
		t.Errorf("Expected 1 disconnect, got %d", metrics.disconnects)
	}
}

func TestBridge_ConnectionDropAndReconnect(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "test-token"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}

	connectChan := make(chan struct{}, 2)
	disconnectChan := make(chan struct{}, 1)

	var connCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()
		if atomic.AddInt32(&connCount, 1) == 1 {
			// First connection: immediately close it to trigger a reconnect.
			conn.Close()
		} else {
			// Second connection: keep it open.
			<-r.Context().Done()
		}
	}))
	defer server.Close()

	handler := &mockHandler{
		onConnect:    func(send func(message []byte) error) { connectChan <- struct{}{} },
		onDisconnect: func(err error) { disconnectChan <- struct{}{} },
	}

	metrics := &mockMetrics{}
	retryPolicy := RetryPolicy{MinBackoff: 50 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, Jitter: 10 * time.Millisecond}
	bridge := New(authClient, WithRetryPolicy(retryPolicy), WithMetrics(metrics))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go bridge.MaintainWebSocket(ctx, "conn-123", "ws"+server.URL[4:], handler)

	// Wait for the sequence of events.
	<-connectChan
	<-disconnectChan
	<-connectChan

	if atomic.LoadInt32(&metrics.connections) != 2 {
		t.Errorf("Expected 2 connections, got %d", metrics.connections)
	}
	if atomic.LoadInt32(&metrics.disconnects) != 1 {
		t.Errorf("Expected 1 disconnect, got %d", metrics.disconnects)
	}
}

func TestBridge_ContextCancellation(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "test-token"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader.Upgrade(w, r, nil)
	}))
	defer server.Close()

	metrics := &mockMetrics{}
	bridge := New(authClient, WithMetrics(metrics))
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error, 1)

	go func() {
		errChan <- bridge.MaintainWebSocket(ctx, "conn-123", "ws"+server.URL[4:], handler)
	}()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-errChan:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, but got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Bridge did not exit after context cancellation")
	}
	if metrics.connectionStatus.Load() != 0.0 {
		t.Errorf("Expected connection status to be 0, but got %v", metrics.connectionStatus.Load())
	}
}

func TestBridge_HappyPath(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "test-token"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}

	connectChan := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()
		<-r.Context().Done()
	}))
	defer server.Close()

	handler := &mockHandler{
		onConnect: func(send func(message []byte) error) { close(connectChan) },
	}

	metrics := &mockMetrics{}
	bridge := New(authClient, WithMetrics(metrics))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go bridge.MaintainWebSocket(ctx, "conn-123", "ws"+server.URL[4:], handler)

	<-connectChan

	if atomic.LoadInt32(&metrics.connections) != 1 {
		t.Errorf("Expected 1 connection, got %d", metrics.connections)
	}
	if metrics.connectionStatus.Load() != 1.0 {
		t.Errorf("Expected connection status to be 1, but got %v", metrics.connectionStatus.Load())
	}
}

func TestBridge_MessageSizeLimit(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "test-token"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}

	disconnectChan := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Send a message that is larger than the configured limit.
		err = conn.WriteMessage(websocket.TextMessage, make([]byte, 2048))
		if err != nil {
			t.Logf("Server write error: %v", err)
		}
	}))
	defer server.Close()

	handler := &mockHandler{
		onDisconnect: func(err error) { disconnectChan <- err },
	}

	bridge := New(authClient, WithMessageSizeLimit(1024))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go bridge.MaintainWebSocket(ctx, "conn-123", "ws"+server.URL[4:], handler)

	select {
	case err := <-disconnectChan:
		if err == nil {
			t.Fatal("Expected an error due to message size limit, but got nil")
		}
		t.Logf("Got expected disconnect error: %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("Bridge did not disconnect after receiving oversized message")
	}
}

func TestBridge_Options(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{}
	bridge := New(authClient,
		WithMessageSizeLimit(1234),
		WithWriteTimeout(5*time.Second),
		WithPingInterval(45*time.Second),
	)

	if bridge.messageSizeLimit != 1234 {
		t.Errorf("Expected messageSizeLimit to be 1234, got %d", bridge.messageSizeLimit)
	}
	if bridge.writeTimeout != 5*time.Second {
		t.Errorf("Expected writeTimeout to be 5s, got %v", bridge.writeTimeout)
	}
	if bridge.pingInterval != 45*time.Second {
		t.Errorf("Expected pingInterval to be 45s, got %v", bridge.pingInterval)
	}
}

func TestBridge_TokenRefreshWithoutDisconnect(t *testing.T) {
	t.Parallel()

	connectChan := make(chan struct{}, 1)
	disconnectChan := make(chan error, 1)
	refreshChan := make(chan struct{}) // Unbuffered channel

	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			// Initial token expires soon.
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "initial-token"},
				ExpiresAt:   time.Now().Add(2 * time.Second).Unix(),
			}, nil
		},
		refreshConnectionFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			refreshChan <- struct{}{}
			// Refreshed token has a long expiry.
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "refreshed-token"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()
		<-r.Context().Done() // Keep connection open until context is cancelled.
	}))
	defer server.Close()

	handler := &mockHandler{
		onConnect:    func(send func(message []byte) error) { connectChan <- struct{}{} },
		onDisconnect: func(err error) { disconnectChan <- err },
	}

	metrics := &mockMetrics{}
	logger := &testLogger{t: t}

	// Use a short refresh buffer to ensure the refresh happens quickly.
	bridge := New(authClient, WithMetrics(metrics), WithRefreshBuffer(100*time.Millisecond), WithLogger(logger))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go bridge.MaintainWebSocket(ctx, "conn-123", "ws"+server.URL[4:], handler)

	// 1. Wait for initial connection
	select {
	case <-connectChan:
		// Good, connected.
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for initial connection")
	}

	// 2. Wait for the token refresh to happen
	select {
	case <-refreshChan:
		// Good, refresh was called.
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for token refresh")
	}

	// 3. Ensure no disconnect happened
	select {
	case err := <-disconnectChan:
		t.Fatalf("OnDisconnect was called unexpectedly: %v", err)
	default:
		// Good, no disconnect.
	}

	// 4. Verify metrics
	if atomic.LoadInt32(&metrics.connections) != 1 {
		t.Errorf("Expected 1 connection, got %d", metrics.connections)
	}
	if atomic.LoadInt32(&metrics.disconnects) != 0 {
		t.Errorf("Expected 0 disconnects, got %d", metrics.disconnects)
	}
	if atomic.LoadInt32(&metrics.tokenRefreshes) != 1 {
		t.Errorf("Expected 1 token refresh, got %d", metrics.tokenRefreshes)
	}
	if metrics.connectionStatus.Load() != 1.0 {
		t.Errorf("Expected connection status to be 1, but got %v", metrics.connectionStatus.Load())
	}
}

// --- gRPC retry loop tests ---

func grpcRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MinBackoff: 10 * time.Millisecond,
		MaxBackoff: 100 * time.Millisecond,
		Jitter:     5 * time.Millisecond,
	}
}

func TestGRPC_CleanExit(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "tok"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}

	metrics := &mockMetrics{}
	b := New(authClient, WithMetrics(metrics), WithRetryPolicy(grpcRetryPolicy()), WithLogger(&testLogger{t: t}))

	run := func(ctx context.Context, conn *grpc.ClientConn) error {
		return nil
	}

	err := b.MaintainGRPCConnection(context.Background(), "conn-1", "passthrough:///localhost:0",
		run, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("expected nil error on clean exit, got: %v", err)
	}
	if atomic.LoadInt32(&metrics.connections) != 1 {
		t.Errorf("expected 1 connection, got %d", metrics.connections)
	}
}

func TestGRPC_PermanentError(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "tok"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}

	metrics := &mockMetrics{}
	b := New(authClient, WithMetrics(metrics), WithRetryPolicy(grpcRetryPolicy()), WithLogger(&testLogger{t: t}))

	run := func(ctx context.Context, conn *grpc.ClientConn) error {
		return NewPermanentError(fmt.Errorf("fatal"))
	}

	err := b.MaintainGRPCConnection(context.Background(), "conn-1", "passthrough:///localhost:0",
		run, grpc.WithTransportCredentials(insecure.NewCredentials()))

	var permErr *PermanentError
	if !errors.As(err, &permErr) {
		t.Fatalf("expected PermanentError, got: %v", err)
	}
}

func TestGRPC_InteractionRequired(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "tok"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}

	metrics := &mockMetrics{}
	b := New(authClient, WithMetrics(metrics), WithRetryPolicy(grpcRetryPolicy()), WithLogger(&testLogger{t: t}))

	run := func(ctx context.Context, conn *grpc.ClientConn) error {
		return ErrInteractionRequired
	}

	err := b.MaintainGRPCConnection(context.Background(), "conn-1", "passthrough:///localhost:0",
		run, grpc.WithTransportCredentials(insecure.NewCredentials()))

	if !errors.Is(err, ErrInteractionRequired) {
		t.Fatalf("expected ErrInteractionRequired, got: %v", err)
	}
	if atomic.LoadInt32(&metrics.connections) != 1 {
		t.Errorf("expected exactly 1 connection (no retry), got %d", metrics.connections)
	}
}

func TestGRPC_RetryThenSucceed(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "tok"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}

	var callCount int32
	metrics := &mockMetrics{}
	b := New(authClient, WithMetrics(metrics), WithRetryPolicy(grpcRetryPolicy()), WithLogger(&testLogger{t: t}))

	run := func(ctx context.Context, conn *grpc.ClientConn) error {
		n := atomic.AddInt32(&callCount, 1)
		if n < 3 {
			return fmt.Errorf("transient error #%d", n)
		}
		return nil
	}

	err := b.MaintainGRPCConnection(context.Background(), "conn-1", "passthrough:///localhost:0",
		run, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("expected nil after retries, got: %v", err)
	}
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("expected run called 3 times, got %d", callCount)
	}
	if atomic.LoadInt32(&metrics.connections) != 3 {
		t.Errorf("expected 3 connections, got %d", metrics.connections)
	}
}

func TestGRPC_ContextCancelledDuringBackoff(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "tok"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}

	b := New(authClient, WithRetryPolicy(RetryPolicy{
		MinBackoff: 10 * time.Second,
		MaxBackoff: 10 * time.Second,
		Jitter:     0,
	}), WithLogger(&testLogger{t: t}))

	var called int32
	run := func(ctx context.Context, conn *grpc.ClientConn) error {
		atomic.AddInt32(&called, 1)
		return fmt.Errorf("transient")
	}

	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error, 1)

	go func() {
		errChan <- b.MaintainGRPCConnection(ctx, "conn-1", "passthrough:///localhost:0",
			run, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errChan:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not exit promptly after context cancellation during backoff")
	}

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("expected run called once before cancel, got %d", called)
	}
}

func TestGRPC_BackoffGrowsExponentially(t *testing.T) {
	t.Parallel()
	authClient := &mockTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "tok"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}

	b := New(authClient, WithRetryPolicy(RetryPolicy{
		MinBackoff: 20 * time.Millisecond,
		MaxBackoff: 500 * time.Millisecond,
		Jitter:     0,
	}), WithLogger(&testLogger{t: t}))

	var timestamps []time.Time
	var callCount int32

	run := func(ctx context.Context, conn *grpc.ClientConn) error {
		timestamps = append(timestamps, time.Now())
		n := atomic.AddInt32(&callCount, 1)
		if n >= 4 {
			return nil
		}
		return fmt.Errorf("transient")
	}

	err := b.MaintainGRPCConnection(context.Background(), "conn-1", "passthrough:///localhost:0",
		run, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(timestamps) < 4 {
		t.Fatalf("expected at least 4 run calls, got %d", len(timestamps))
	}

	// Gaps between calls 2→3 and 3→4 should grow (backoff doubles each time).
	for i := 2; i < len(timestamps); i++ {
		prevGap := timestamps[i-1].Sub(timestamps[i-2])
		thisGap := timestamps[i].Sub(timestamps[i-1])
		if thisGap < prevGap {
			t.Errorf("backoff did not grow: gap[%d]=%v < gap[%d]=%v", i, thisGap, i-1, prevGap)
		}
	}
}
