package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestAPIKey_HTTP(t *testing.T) {
	src := NewStaticAPIKeySource(map[string]struct{}{"good": {}})
	h := APIKey(true, src, "/health", "/auth/callback")(okHandler())

	cases := []struct {
		name string
		path string
		key  string
		want int
	}{
		{"missing key", "/v1/providers", "", http.StatusUnauthorized},
		{"wrong key", "/v1/providers", "bad", http.StatusForbidden},
		{"right key", "/v1/providers", "good", http.StatusOK},
		{"key with whitespace", "/v1/providers", "  good ", http.StatusOK},
		{"health is public", "/health", "", http.StatusOK},
		{"callback is public", "/auth/callback", "", http.StatusOK},
		{"public path does not cover sub paths", "/health/x", "", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, c.path, nil)
			if c.key != "" {
				r.Header.Set(APIKeyHeader, c.key)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != c.want {
				t.Fatalf("%s: got %d, want %d", c.name, w.Code, c.want)
			}
		})
	}
}

func TestAPIKey_NotRequiredPassesEverything(t *testing.T) {
	h := APIKey(false, nil)(okHandler())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/token/x", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
}

func TestAPIKey_RequiredWithNilSourceRejects(t *testing.T) {
	h := APIKey(true, nil)(okHandler())
	r := httptest.NewRequest(http.MethodGet, "/v1/token/x", nil)
	r.Header.Set(APIKeyHeader, "anything")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
}

func TestReloadingAPIKeySource(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "keys")
	if err := os.WriteFile(file, []byte("file-1,file-2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := NewReloadingAPIKeySource(map[string]struct{}{"inline": {}}, []string{file}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	src.now = func() time.Time { return now }
	for _, k := range []string{"inline", "file-1", "file-2"} {
		if !src.Contains(k) {
			t.Fatalf("expected %q to be allowed", k)
		}
	}
	if src.Len() != 3 {
		t.Fatalf("Len = %d, want 3", src.Len())
	}

	// Rotate the file; the old key is still accepted until the interval passes.
	if err := os.WriteFile(file, []byte("file-3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !src.Contains("file-1") || src.Contains("file-3") {
		t.Fatal("reload must not happen before the interval")
	}
	now = now.Add(2 * time.Minute)
	if src.Contains("file-1") || !src.Contains("file-3") {
		t.Fatal("reload must swap in the new file keys after the interval")
	}

	// A broken file keeps the last good set.
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if !src.Contains("file-3") {
		t.Fatal("a failed reload must keep the previous keys")
	}
}

func TestNewReloadingAPIKeySource_MissingFileFailsAtStartup(t *testing.T) {
	if _, err := NewReloadingAPIKeySource(nil, []string{"/nonexistent/keys"}, time.Minute); err == nil {
		t.Fatal("expected an error for a missing key file")
	}
}

func TestAPIKeyUnaryInterceptor(t *testing.T) {
	src := NewStaticAPIKeySource(map[string]struct{}{"good": {}})
	ic := APIKeyUnaryInterceptor(true, src)
	next := func(ctx context.Context, req interface{}) (interface{}, error) { return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/nexus.v1.NexusService/GetToken"}

	call := func(md metadata.MD) codes.Code {
		ctx := metadata.NewIncomingContext(context.Background(), md)
		_, err := ic(ctx, nil, info, next)
		return status.Code(err)
	}
	if got := call(metadata.MD{}); got != codes.Unauthenticated {
		t.Fatalf("no key: %v", got)
	}
	if got := call(metadata.Pairs("x-api-key", "bad")); got != codes.PermissionDenied {
		t.Fatalf("bad key: %v", got)
	}
	if got := call(metadata.Pairs("x-api-key", "good")); got != codes.OK {
		t.Fatalf("direct grpc key: %v", got)
	}
	if got := call(metadata.Pairs("grpcgateway-x-api-key", "good")); got != codes.OK {
		t.Fatalf("grpc-gateway forwarded key: %v", got)
	}
	off := APIKeyUnaryInterceptor(false, nil)
	if _, err := off(context.Background(), nil, info, next); err != nil {
		t.Fatalf("not required: %v", err)
	}
}
