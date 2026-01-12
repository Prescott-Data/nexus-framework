package bridge_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"bitbucket.org/dromos/oauth-framework/oauth-sdk"
	"dromos.io/bridge"
	"dromos.io/bridge/internal/auth"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// SDKAdapter adapts the oauthsdk.Client to the bridge.OAuthClient interface.
type SDKAdapter struct {
	client *oauthsdk.Client
}

func (a *SDKAdapter) GetToken(ctx context.Context, connectionID string) (*bridge.Token, error) {
	resp, err := a.client.GetToken(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	return a.convertToken(resp), nil
}

func (a *SDKAdapter) RefreshConnection(ctx context.Context, connectionID string) (*bridge.Token, error) {
	resp, err := a.client.RefreshConnection(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	return a.convertToken(resp), nil
}

func (a *SDKAdapter) convertToken(resp *oauthsdk.TokenResponse) *bridge.Token {
	var strategy auth.AuthStrategy
	if resp.Strategy != nil {
		strategy.Type, _ = resp.Strategy["type"].(string)
		if cfg, ok := resp.Strategy["config"].(map[string]interface{}); ok {
			strategy.Config = cfg
		}
	}

	var creds auth.Credentials
	if resp.Credentials != nil {
		creds = auth.Credentials(resp.Credentials)
	}

	return &bridge.Token{
		Strategy:    strategy,
		Credentials: creds,
		ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
	}
}

// MockBroker represents the Dromos Gateway/Broker API.
type MockBroker struct {
	server    *httptest.Server
	responses map[string]map[string]interface{}
	mu        sync.RWMutex
}

func NewMockBroker() *MockBroker {
	mb := &MockBroker{
		responses: make(map[string]map[string]interface{}),
	}
	mb.server = httptest.NewServer(http.HandlerFunc(mb.handleTokenRequest))
	return mb
}

func (mb *MockBroker) URL() string {
	return mb.server.URL
}

func (mb *MockBroker) Close() {
	mb.server.Close()
}

func (mb *MockBroker) SetResponse(connectionID string, resp map[string]interface{}) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.responses[connectionID] = resp
}

func (mb *MockBroker) handleTokenRequest(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	connectionID := parts[3]

	mb.mu.RLock()
	resp, ok := mb.responses[connectionID]
	mb.mu.RUnlock()

	if !ok {
		http.Error(w, "Connection not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// MockWSProvider represents an external WebSocket service.
type MockWSProvider struct {
	server    *httptest.Server
	validator func(*http.Request) error
	upgrader  websocket.Upgrader
}

func NewMockWSProvider() *MockWSProvider {
	mp := &MockWSProvider{
		upgrader: websocket.Upgrader{},
	}
	mp.server = httptest.NewServer(http.HandlerFunc(mp.handleWebSocket))
	return mp
}

func (mp *MockWSProvider) URL() string {
	return "ws" + strings.TrimPrefix(mp.server.URL, "http")
}

func (mp *MockWSProvider) Close() {
	mp.server.Close()
}

func (mp *MockWSProvider) SetValidator(v func(*http.Request) error) {
	mp.validator = v
}

func (mp *MockWSProvider) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if mp.validator != nil {
		if err := mp.validator(r); err != nil {
			http.Error(w, "Auth failed: "+err.Error(), http.StatusUnauthorized)
			return
		}
	}
	conn, err := mp.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	time.Sleep(100 * time.Millisecond)
}

// MockGRPCProvider represents an external gRPC service.
type MockGRPCProvider struct {
	server   *grpc.Server
	listener net.Listener
}

func NewMockGRPCProvider(interceptor grpc.UnaryServerInterceptor) (*MockGRPCProvider, error) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	opts := []grpc.ServerOption{grpc.UnaryInterceptor(interceptor)}
	s := grpc.NewServer(opts...)

	// Register the standard health service
	healthService := health.NewServer()
	grpc_health_v1.RegisterHealthServer(s, healthService)
	healthService.SetServingStatus("test-service", grpc_health_v1.HealthCheckResponse_SERVING)

	go s.Serve(lis)

	return &MockGRPCProvider{
		server:   s,
		listener: lis,
	}, nil
}

func (p *MockGRPCProvider) Addr() string {
	return p.listener.Addr().String()
}

func (p *MockGRPCProvider) Close() {
	p.server.Stop()
	p.listener.Close()
}

// --- Integration Tests ---

func TestBridge_Integration_WebSocket(t *testing.T) {
	// 1. Start Mocks
	broker := NewMockBroker()
	defer broker.Close()
	provider := NewMockWSProvider()
	defer provider.Close()

	// 2. Setup Bridge Client
	sdkClient := oauthsdk.New(broker.URL())
	adapter := &SDKAdapter{client: sdkClient}

	tests := []struct {
		name         string
		connectionID string
		brokerResp   map[string]interface{}
		validate     func(*http.Request) error
	}{
		// ... WebSocket test cases from before ...
		{
			name:         "Basic Auth",
			connectionID: "conn-basic",
			brokerResp: map[string]interface{}{
				"strategy":    map[string]interface{}{"type": "basic_auth"},
				"credentials": map[string]interface{}{"username": "admin", "password": "123"},
			},
			validate: func(r *http.Request) error {
				u, p, ok := r.BasicAuth()
				if !ok || u != "admin" || p != "123" {
					return fmt.Errorf("bad basic auth")
				}
				return nil
			},
		},
		{
			name:         "Query Param",
			connectionID: "conn-query",
			brokerResp: map[string]interface{}{
				"strategy":    map[string]interface{}{"type": "query_param", "config": map[string]interface{}{"param_name": "key"}},
				"credentials": map[string]interface{}{"api_key": "secret"},
			},
			validate: func(r *http.Request) error {
				if r.URL.Query().Get("key") != "secret" {
					return fmt.Errorf("bad query param")
				}
				return nil
			},
		},
		{
			name:         "HMAC",
			connectionID: "conn-hmac",
			brokerResp: map[string]interface{}{
				"strategy":    map[string]interface{}{"type": "hmac_payload", "config": map[string]interface{}{"header_name": "X-Sig"}},
				"credentials": map[string]interface{}{"api_secret": "hmac-secret"},
			},
			validate: func(r *http.Request) error {
				mac := hmac.New(sha256.New, []byte("hmac-secret"))
				mac.Write([]byte{})
				expected := hex.EncodeToString(mac.Sum(nil))
				if r.Header.Get("X-Sig") != expected {
					return fmt.Errorf("bad hmac sig")
				}
				return nil
			},
		},
		{
			name:         "AWS SigV4",
			connectionID: "conn-aws",
			brokerResp: map[string]interface{}{
				"strategy":    map[string]interface{}{"type": "aws_sigv4", "config": map[string]interface{}{"service": "execute-api", "region": "us-east-1"}},
				"credentials": map[string]interface{}{"access_key": "AKID", "secret_key": "SECRET"},
			},
			validate: func(r *http.Request) error {
				if !strings.Contains(r.Header.Get("Authorization"), "Credential=AKID") {
					return fmt.Errorf("bad aws auth")
				}
				return nil
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			broker.SetResponse(tc.connectionID, tc.brokerResp)
			successChan := make(chan struct{})
			provider.SetValidator(func(r *http.Request) error {
				err := tc.validate(r)
				if err == nil {
					close(successChan)
				}
				return err
			})

			b := bridge.New(adapter)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			go func() {
				b.MaintainWebSocket(ctx, tc.connectionID, provider.URL(), &noopHandler{})
			}()

			select {
			case <-successChan:
			case <-ctx.Done():
				t.Fatal("Timeout waiting for successful ws validation")
			}
		})
	}
}

func TestBridge_Integration_GRPC(t *testing.T) {
	// 1. Setup Mocks
	broker := NewMockBroker()
	defer broker.Close()

		successChan := make(chan struct{})

		interceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {

			md, ok := metadata.FromIncomingContext(ctx)

			if !ok {

				return nil, status.Errorf(codes.Unauthenticated, "missing metadata")

			}

			t.Logf("gRPC Interceptor received metadata: %v", md)

			if len(md.Get("authorization")) == 0 || md.Get("authorization")[0] != "Bearer grpc-secret-token" {

				return nil, status.Errorf(codes.Unauthenticated, "invalid token")

			}

			// If validation is successful, signal it and call the handler

			close(successChan)

			return handler(ctx, req)

		}

	

		provider, err := NewMockGRPCProvider(interceptor)

		if err != nil {

			t.Fatalf("Failed to create mock gRPC provider: %v", err)

		}

		defer provider.Close()

	

		// 2. Configure Broker Response

		broker.SetResponse("conn-grpc", map[string]interface{}{

			"strategy": map[string]interface{}{

				"type": "oauth2",

			},

			"credentials": map[string]interface{}{

				"access_token": "grpc-secret-token",

			},

		})

	

		// 3. Setup Bridge Client

		sdkClient := oauthsdk.New(broker.URL())

		adapter := &SDKAdapter{client: sdkClient}

		b := bridge.New(adapter, bridge.WithLogger(&testLogger{t: t})) // Add logger to bridge

	

		// 4. Run the Bridge

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

		defer cancel()

	

		runFunc := func(ctx context.Context, conn *grpc.ClientConn) error {

			client := grpc_health_v1.NewHealthClient(conn)

			_, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "test-service"})

			if err != nil {

				t.Logf("Health check failed: %v", err)

				return err

			}

			t.Logf("Health check successful")

			return nil

		}

	

		// Run MaintainGRPCConnection in a goroutine

		go func() {

			err := b.MaintainGRPCConnection(ctx, "conn-grpc", provider.Addr(), runFunc, grpc.WithTransportCredentials(insecure.NewCredentials()))

			if err != nil {

				t.Logf("MaintainGRPCConnection exited with error: %v", err)

			}

		}()

	

		// 5. Wait for validation signal

		select {

		case <-successChan:

			// Test passed!

		case <-ctx.Done():

			t.Fatal("Timeout waiting for successful gRPC call")

		}

	}

	

	type noopHandler struct{}

	

	func (h *noopHandler) OnConnect(send func(message []byte) error) {}

	func (h *noopHandler) OnMessage(message []byte)                  {}

	func (h *noopHandler) OnDisconnect(err error)                    {}

	

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

	