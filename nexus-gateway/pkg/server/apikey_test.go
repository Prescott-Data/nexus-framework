package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"

	gwmiddleware "github.com/Prescott-Data/nexus-framework/nexus-gateway/pkg/middleware"
)

func newKeyedServer(t *testing.T) http.Handler {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	src := gwmiddleware.NewStaticAPIKeySource(map[string]struct{}{"agent-key": {}})
	// The broker URL is unreachable on purpose: a request that passes the key
	// check fails later, with anything but 401 or 403.
	return New("0", "http://127.0.0.1:1", make([]byte, 32), &http.Client{}, rdb, WithAPIKey(true, src)).Handler()
}

func TestServer_APIKeyGatesEverythingButPublicPaths(t *testing.T) {
	h := newKeyedServer(t)
	get := func(path, key string) int {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if key != "" {
			r.Header.Set("X-API-Key", key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if got := get("/health", ""); got != http.StatusOK {
		t.Fatalf("/health without key: %d", got)
	}
	if got := get("/v1/providers", ""); got != http.StatusUnauthorized {
		t.Fatalf("/v1/providers without key: %d, want 401", got)
	}
	if got := get("/v1/providers", "wrong"); got != http.StatusForbidden {
		t.Fatalf("/v1/providers wrong key: %d, want 403", got)
	}
	if got := get("/v1/providers", "agent-key"); got == http.StatusUnauthorized || got == http.StatusForbidden {
		t.Fatalf("/v1/providers right key must pass the gate, got %d", got)
	}
	if got := get("/v1/capture-schema?provider_id=x", ""); got == http.StatusUnauthorized || got == http.StatusForbidden {
		t.Fatalf("capture-schema is browser-facing and must be public, got %d", got)
	}
	if got := get("/metrics", ""); got != http.StatusUnauthorized {
		t.Fatalf("/metrics must need the key, got %d", got)
	}
}
