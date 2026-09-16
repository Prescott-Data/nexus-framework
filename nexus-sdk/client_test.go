package oauthsdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestConnection(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/request-connection", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authUrl":       "http://example/auth",
			"connection_id": "abc",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	out, err := c.RequestConnection(context.Background(), RequestConnectionInput{UserID: "u", ProviderName: "p", Scopes: []string{"s"}, ReturnURL: "http://x"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ConnectionID != "abc" {
		t.Fatalf("want abc, got %s", out.ConnectionID)
	}
}

// RevokeConnection must issue a DELETE to the gateway, carry the optional
// scoping in the query string, and preserve the provider_revoked distinction.
func TestRevokeConnection(t *testing.T) {
	var gotMethod, gotQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/connections/abc", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connection_id":             "abc",
			"provider_name":             "google",
			"status":                    "revoked",
			"token_deleted":             true,
			"provider_revoked":          false,
			"provider_revocation_error": "provider does not advertise a revocation endpoint",
			"sessions_closed":           2,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	out, err := c.RevokeConnection(context.Background(), "abc", &RevokeOptions{
		WorkspaceID: "ws-1",
		Reason:      "offboarding",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("want DELETE, got %s", gotMethod)
	}
	if !strings.Contains(gotQuery, "workspace_id=ws-1") || !strings.Contains(gotQuery, "reason=offboarding") {
		t.Fatalf("unexpected query %q", gotQuery)
	}
	if out.Status != "revoked" || !out.TokenDeleted {
		t.Fatalf("unexpected result %+v", out)
	}
	// The credential is gone locally but the provider was not reached; callers
	// must be able to see that.
	if out.ProviderRevoked {
		t.Fatal("provider_revoked should be false")
	}
	if out.SessionsClosed != 2 {
		t.Fatalf("want 2 sessions closed, got %d", out.SessionsClosed)
	}
}

func TestRevokeConnection_EmptyID(t *testing.T) {
	c := New("http://localhost:1")
	if _, err := c.RevokeConnection(context.Background(), "  ", nil); err == nil {
		t.Fatal("expected an error for an empty connection id")
	}
}

func TestCheckConnection(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/check-connection/abc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "active"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	status, err := c.CheckConnection(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Fatalf("want active, got %s", status)
	}
}

func TestGetToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/token/abc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "xyz", "expires_in": 3600})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	tok, err := c.GetToken(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "xyz" {
		t.Fatalf("want xyz, got %s", tok.AccessToken)
	}
}

func TestWaitForActive(t *testing.T) {
	mux := http.NewServeMux()
	count := 0
	mux.HandleFunc("/v1/check-connection/abc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if count < 2 {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending"})
			count++
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "active"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := c.WaitForActive(ctx, "abc", 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Fatalf("want active, got %s", status)
	}
}
