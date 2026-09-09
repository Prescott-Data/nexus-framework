package usecase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The gateway must forward the revocation to the broker with the connection ID
// in the path and the optional scoping in the query string, and hand the
// broker's result back unmodified.
func TestRevokeConnectionCore_ForwardsToBroker(t *testing.T) {
	const connID = "3f1b0f4e-0b3a-4e5e-9d61-2b8a5f4c1d77"

	var gotPath, gotQuery, gotMethod string
	mux := http.NewServeMux()
	mux.HandleFunc("/connections/", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connection_id":"` + connID + `","status":"revoked","token_deleted":true,"provider_revoked":true,"sessions_closed":1}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	h := NewHandler(srv.URL, []byte("test-key"), srv.Client())

	result, status, err := h.RevokeConnectionCore(context.Background(), RevokeConnectionInput{
		ConnectionID: connID,
		WorkspaceID:  "ws-1",
		Reason:       "offboarding",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("want 200, got %d", status)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("want DELETE, got %s", gotMethod)
	}
	if gotPath != "/connections/"+connID {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if !strings.Contains(gotQuery, "workspace_id=ws-1") || !strings.Contains(gotQuery, "reason=offboarding") {
		t.Fatalf("unexpected query %q", gotQuery)
	}
	if result["status"] != "revoked" || result["provider_revoked"] != true {
		t.Fatalf("unexpected result %v", result)
	}
}

// A non-UUID connection ID must be rejected before any broker call is made.
func TestRevokeConnectionCore_RejectsNonUUID(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := NewHandler(srv.URL, []byte("test-key"), srv.Client())

	_, status, err := h.RevokeConnectionCore(context.Background(), RevokeConnectionInput{ConnectionID: "not-a-uuid"})
	if err == nil {
		t.Fatal("expected an error for a non-UUID connection id")
	}
	if status != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", status)
	}
	if called {
		t.Fatal("broker must not be called for an invalid connection id")
	}
}

// A broker error status is surfaced to the caller rather than masked as success.
func TestRevokeConnectionCore_PropagatesBrokerStatus(t *testing.T) {
	const connID = "3f1b0f4e-0b3a-4e5e-9d61-2b8a5f4c1d77"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	h := NewHandler(srv.URL, []byte("test-key"), srv.Client())

	result, status, err := h.RevokeConnectionCore(context.Background(), RevokeConnectionInput{ConnectionID: connID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusNotFound {
		t.Fatalf("want 404, got %d", status)
	}
	if result != nil {
		t.Fatalf("want nil result, got %v", result)
	}
}
