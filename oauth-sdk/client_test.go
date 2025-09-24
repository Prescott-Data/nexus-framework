package oauthsdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
