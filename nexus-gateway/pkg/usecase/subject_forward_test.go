package usecase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/metadata"

	gwmiddleware "github.com/Prescott-Data/nexus-framework/nexus-gateway/pkg/middleware"
)

// The broker decides whose credential may be released; the gateway carries the
// subject, agent and operation to it unchanged, over HTTP and gRPC alike.
func TestSubjectHeadersReachTheBroker(t *testing.T) {
	seen := map[string]http.Header{}
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Path] = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at"}`))
	}))
	defer broker.Close()
	h := NewHandler(broker.URL, make([]byte, 32), nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/token/11111111-1111-1111-1111-111111111111", nil)
	req.Header.Set("X-Nexus-Subject", "ws-ada")
	req.Header.Set("X-Nexus-Agent", "coder-1")
	req.Header.Set("X-Nexus-Operation", "op-42")
	gwmiddleware.Subject(http.HandlerFunc(h.GetToken)).ServeHTTP(httptest.NewRecorder(), req)

	got := seen["/connections/11111111-1111-1111-1111-111111111111/token"]
	if got == nil {
		t.Fatalf("broker was not called: %v", seen)
	}
	for name, want := range map[string]string{"X-Nexus-Subject": "ws-ada", "X-Nexus-Agent": "coder-1", "X-Nexus-Operation": "op-42"} {
		if got.Get(name) != want {
			t.Errorf("%s = %q, want %q", name, got.Get(name), want)
		}
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-nexus-subject", "ws-bob"))
	_, _ = gwmiddleware.SubjectUnaryInterceptor()(ctx, nil, nil, func(ctx context.Context, _ interface{}) (interface{}, error) {
		_, _, err := h.GetTokenCore(ctx, "22222222-2222-2222-2222-222222222222")
		return nil, err
	})
	if got := seen["/connections/22222222-2222-2222-2222-222222222222/token"]; got.Get("X-Nexus-Subject") != "ws-bob" {
		t.Errorf("gRPC subject not forwarded: %v", got)
	}
}

// A status check asks the broker's status route, never its token route.
func TestCheckConnectionUsesTheStatusRoute(t *testing.T) {
	var paths []string
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connection_id":"x","status":"active"}`))
	}))
	defer broker.Close()
	h := NewHandler(broker.URL, make([]byte, 32), nil)

	status, err := h.CheckConnectionCore(context.Background(), "33333333-3333-3333-3333-333333333333")
	if err != nil || status != "active" {
		t.Fatalf("status %q err %v", status, err)
	}
	if len(paths) != 1 || paths[0] != "/connections/33333333-3333-3333-3333-333333333333/status" {
		t.Fatalf("called %v", paths)
	}
}
