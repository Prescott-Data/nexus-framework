package grpcsrv

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"

	gwmiddleware "github.com/Prescott-Data/nexus-framework/nexus-gateway/pkg/middleware"
	"github.com/Prescott-Data/nexus-framework/nexus-gateway/pkg/usecase"
)

// Exercises the real transcoding path on a route NexusService actually
// serves (GET /v1/token/{id}): HTTP -> grpc-gateway mux -> gRPC server ->
// interceptor. If the mux failed to forward the header, the interceptor
// would answer Unauthenticated, which grpc-gateway maps to 401, and the
// right-key case would fail.
func TestHTTPGateway_APIKeyReachesInterceptor(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	src := gwmiddleware.NewStaticAPIKeySource(map[string]struct{}{"agent-key": {}})
	// Unreachable broker on purpose: a request that passes both checks fails later with anything but 401/403.
	h := usecase.NewHandler("http://127.0.0.1:1", make([]byte, 32), &http.Client{Timeout: time.Second})

	srv, err := NewServer(Options{
		GRPCAddress:   "127.0.0.1:0",
		HTTPAddress:   "127.0.0.1:0",
		Handler:       h,
		RedisClient:   rdb,
		RequireAPIKey: true,
		APIKeys:       src,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Shutdown(context.Background()) }()

	get := func(key string) int {
		req, _ := http.NewRequest(http.MethodGet, "http://"+srv.HTTPAddress()+"/v1/token/conn-1", nil)
		if key != "" {
			req.Header.Set("X-API-Key", key)
		}
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := get(""); got != http.StatusUnauthorized {
		t.Fatalf("no key: %d, want 401", got)
	}
	if got := get("wrong"); got != http.StatusForbidden {
		t.Fatalf("wrong key: %d, want 403", got)
	}
	if got := get("agent-key"); got == http.StatusUnauthorized || got == http.StatusForbidden || got == http.StatusNotFound {
		t.Fatalf("right key must pass both the HTTP middleware and the interceptor and reach the handler, got %d", got)
	}
}
