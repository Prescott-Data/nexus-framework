package grpcsrv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"

	"github.com/Prescott-Data/nexus-framework/nexus-gateway/pkg/usecase"
)

// The transcoding listener (REST -> grpc-gateway -> gRPC) must carry whom a
// credential is for to the broker, as the plain REST server does.
func TestHTTPGateway_SubjectReachesTheBroker(t *testing.T) {
	seen := make(chan http.Header, 1)
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at"}`))
	}))
	defer broker.Close()
	mr := miniredis.RunT(t)
	srv, err := NewServer(Options{
		GRPCAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0",
		Handler:     usecase.NewHandler(broker.URL, make([]byte, 32), &http.Client{Timeout: time.Second}),
		RedisClient: redis.NewClient(&redis.Options{Addr: mr.Addr()}),
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

	req, _ := http.NewRequest(http.MethodGet, "http://"+srv.HTTPAddress()+"/v1/token/11111111-1111-1111-1111-111111111111", nil)
	req.Header.Set("X-Nexus-Subject", "ws-ada")
	req.Header.Set("X-Nexus-Agent", "coder-1")
	req.Header.Set("X-Nexus-Operation", "op-42")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	select {
	case got := <-seen:
		for name, want := range map[string]string{"X-Nexus-Subject": "ws-ada", "X-Nexus-Agent": "coder-1", "X-Nexus-Operation": "op-42"} {
			if got.Get(name) != want {
				t.Errorf("%s = %q at the broker, want %q", name, got.Get(name), want)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the broker was never called")
	}
}
