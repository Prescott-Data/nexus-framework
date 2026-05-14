package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

var nextHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestRateLimiter(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	t.Run("Disabled by default", func(t *testing.T) {
		t.Setenv("RATE_LIMIT_ENABLED", "false")

		middleware := RateLimiter(redisClient)
		handler := middleware(nextHandler)

		req := httptest.NewRequest("POST", "/v1/request-connection", nil)
		req = req.WithContext(ContextWithWorkspace(req.Context(), "ws_1"))

		// Fire 50 requests
		for i := 0; i < 50; i++ {
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200 OK, got %d", rr.Code)
			}
		}
	})

	t.Run("Enforces Rate Limit on request-connection", func(t *testing.T) {
		t.Setenv("RATE_LIMIT_ENABLED", "true")
		t.Setenv("RATE_LIMIT_REQUEST_CONNECTION_RPM", "5")

		middleware := RateLimiter(redisClient)
		handler := middleware(nextHandler)

		req := httptest.NewRequest("POST", "/v1/request-connection", nil)
		req = req.WithContext(ContextWithWorkspace(req.Context(), "ws_2"))

		// First 5 should succeed
		for i := 0; i < 5; i++ {
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200 OK on request %d, got %d", i+1, rr.Code)
			}
		}

		// 6th should fail with 429
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429 Too Many Requests, got %d", rr.Code)
		}

		if retryAfter := rr.Header().Get("Retry-After"); retryAfter == "" {
			t.Error("expected Retry-After header to be set")
		}
	})

	t.Run("Different Workspaces have different buckets", func(t *testing.T) {
		t.Setenv("RATE_LIMIT_ENABLED", "true")
		t.Setenv("RATE_LIMIT_REQUEST_CONNECTION_RPM", "5")

		middleware := RateLimiter(redisClient)
		handler := middleware(nextHandler)

		reqA := httptest.NewRequest("POST", "/v1/request-connection", nil)
		reqA = reqA.WithContext(ContextWithWorkspace(reqA.Context(), "ws_A"))

		reqB := httptest.NewRequest("POST", "/v1/request-connection", nil)
		reqB = reqB.WithContext(ContextWithWorkspace(reqB.Context(), "ws_B"))

		// Exhaust A
		for i := 0; i < 5; i++ {
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, reqA)
		}

		// Next A fails
		rrA := httptest.NewRecorder()
		handler.ServeHTTP(rrA, reqA)
		if rrA.Code != http.StatusTooManyRequests {
			t.Errorf("expected A to be rate limited")
		}

		// B should still succeed
		rrB := httptest.NewRecorder()
		handler.ServeHTTP(rrB, reqB)
		if rrB.Code != http.StatusOK {
			t.Errorf("expected B to succeed, got %d", rrB.Code)
		}
	})

	t.Run("Fails open if Redis is down", func(t *testing.T) {
		t.Setenv("RATE_LIMIT_ENABLED", "true")
		t.Setenv("RATE_LIMIT_REQUEST_CONNECTION_RPM", "5")

		addr := mr.Addr()
		// Close the server to simulate downtime
		mr.Close()

		// Try to use a broken client
		brokenClient := redis.NewClient(&redis.Options{Addr: addr})

		middleware := RateLimiter(brokenClient)
		handler := middleware(nextHandler)

		req := httptest.NewRequest("POST", "/v1/request-connection", nil)

		// 10 requests, should all pass even though limit is 5 because redis is unreachable
		for i := 0; i < 10; i++ {
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200 OK because of fail-open, got %d", rr.Code)
			}
		}
	})
}

func TestExtractWorkspaceID(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*http.Request) *http.Request
		expected string
	}{
		{
			name: "From Context (workspaceIDKey)",
			setup: func(r *http.Request) *http.Request {
				return r.WithContext(context.WithValue(r.Context(), workspaceIDKey, "ctx-ws-1"))
			},
			expected: "ctx-ws-1",
		},
		{
			name: "Bare string key no longer matches (must use typed key)",
			setup: func(r *http.Request) *http.Request {
				return r.WithContext(context.WithValue(r.Context(), "workspace_id", "ctx-ws-str"))
			},
			expected: "",
		},
		{
			name: "From Header",
			setup: func(r *http.Request) *http.Request {
				r.Header.Set("X-Workspace-Id", "hdr-ws-2")
				return r
			},
			expected: "hdr-ws-2",
		},
		{
			name: "From Query",
			setup: func(r *http.Request) *http.Request {
				r.URL.RawQuery = "workspace_id=qry-ws-3"
				return r
			},
			expected: "qry-ws-3",
		},
		{
			name: "None",
			setup: func(r *http.Request) *http.Request {
				return r
			},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req = tc.setup(req)
			actual := extractWorkspaceID(req)
			if actual != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}
