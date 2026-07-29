package oauthsdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResolveToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resolve", func(w http.ResponseWriter, r *http.Request) {
		wid := r.URL.Query().Get("workspace_id")
		prov := r.URL.Query().Get("provider_name")
		if wid != "ws-001" || prov != "github" {
			t.Fatalf("unexpected params: workspace_id=%s, provider_name=%s", wid, prov)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "gho_abc123",
			"token_type":   "bearer",
			"credentials": map[string]any{
				"access_token": "gho_abc123",
				"token_type":   "bearer",
				"expires_at":   "2026-12-31T23:59:59Z",
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	tok, err := c.ResolveToken(context.Background(), "ws-001", "github")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "gho_abc123" {
		t.Fatalf("want gho_abc123, got %s", tok.AccessToken)
	}
	if tok.TokenType != "bearer" {
		t.Fatalf("want bearer, got %s", tok.TokenType)
	}
	if tok.ExpiresAt == nil {
		t.Fatal("expected expires_at to be set")
	}
}

func TestResolveToken_SelfHostedBaseURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resolve", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "k",
			"credentials":  map[string]any{"access_token": "k", "api_key": "k"},
			"strategy":     map[string]any{"type": "header"},
			"api_base_url": "https://jenkins.acme.internal",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	tok, err := c.ResolveToken(context.Background(), "ws-001", "jenkins")
	if err != nil {
		t.Fatal(err)
	}
	if tok.APIBaseURL != "https://jenkins.acme.internal" {
		t.Fatalf("want self-hosted base url, got %q", tok.APIBaseURL)
	}
}

func TestResolveToken_MissingParams(t *testing.T) {
	c := New("http://localhost")
	_, err := c.ResolveToken(context.Background(), "", "github")
	if err == nil {
		t.Fatal("expected error for empty workspace_id")
	}
	_, err = c.ResolveToken(context.Background(), "ws", "")
	if err == nil {
		t.Fatal("expected error for empty provider_name")
	}
}

func TestTokenCache(t *testing.T) {
	cache := NewTokenCache(30 * time.Second)

	// Empty cache returns nil
	if got := cache.Get("ws", "gh"); got != nil {
		t.Fatal("expected nil for empty cache")
	}

	// Set and get
	cache.Set("ws", "gh", CachedToken{
		AccessToken: "tok1",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	})

	got := cache.Get("ws", "gh")
	if got == nil {
		t.Fatal("expected cached token")
	}
	if got.AccessToken != "tok1" {
		t.Fatalf("want tok1, got %s", got.AccessToken)
	}

	// Expired token returns nil
	cache.Set("ws", "expired", CachedToken{
		AccessToken: "old",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(-1 * time.Minute),
	})
	if got := cache.Get("ws", "expired"); got != nil {
		t.Fatal("expected nil for expired token")
	}

	// Delete
	cache.Delete("ws", "gh")
	if got := cache.Get("ws", "gh"); got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestGetCachedToken(t *testing.T) {
	callCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resolve", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fresh-token",
			"token_type":   "Bearer",
			"credentials": map[string]any{
				"access_token": "fresh-token",
				"token_type":   "Bearer",
				"expires_at":   time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	cache := NewTokenCache(30 * time.Second)

	// First call should hit the server
	tok1, err := c.GetCachedToken(context.Background(), cache, "ws", "gh")
	if err != nil {
		t.Fatal(err)
	}
	if tok1.AccessToken != "fresh-token" {
		t.Fatalf("want fresh-token, got %s", tok1.AccessToken)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 server call, got %d", callCount)
	}

	// Second call should use cache
	tok2, err := c.GetCachedToken(context.Background(), cache, "ws", "gh")
	if err != nil {
		t.Fatal(err)
	}
	if tok2.AccessToken != "fresh-token" {
		t.Fatalf("want fresh-token, got %s", tok2.AccessToken)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 server call (cached), got %d", callCount)
	}
}

func TestAuthenticatedTransport(t *testing.T) {
	// Mock the gateway /v1/resolve
	gateway := http.NewServeMux()
	gateway.HandleFunc("/v1/resolve", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "injected-token",
			"token_type":   "bearer",
			"credentials": map[string]any{
				"access_token": "injected-token",
				"token_type":   "bearer",
				"expires_at":   time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			},
		})
	})
	gatewaySrv := httptest.NewServer(gateway)
	defer gatewaySrv.Close()

	// Mock an upstream API that checks for the Authorization header
	upstream := http.NewServeMux()
	upstream.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer injected-token" {
			t.Fatalf("expected 'Bearer injected-token', got '%s'", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "testuser"})
	})
	upstreamSrv := httptest.NewServer(upstream)
	defer upstreamSrv.Close()

	// Create the authenticated client
	nexusClient := New(gatewaySrv.URL)
	httpClient := nexusClient.AuthenticatedHTTPClient(
		NewTokenCache(30*time.Second),
		"ws-001",
		"github",
	)

	// Make a request through the authenticated client
	resp, err := httpClient.Get(upstreamSrv.URL + "/user")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["login"] != "testuser" {
		t.Fatalf("want testuser, got %v", body["login"])
	}
}
