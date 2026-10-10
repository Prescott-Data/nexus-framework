package oauthsdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSendsAPIKeyToGateway(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, WithAPIKey("agent-key"), WithBroker("http://broker.internal", "broker-admin-key"))
	resp, err := c.do(context.Background(), http.MethodGet, srv.URL+"/v1/providers", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got != "agent-key" {
		t.Fatalf("X-API-Key = %q, want the gateway key, never the broker key", got)
	}
}

func TestRejectedKeyIsAStructuredError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"invalid_api_key","message":"invalid api key"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, WithAPIKey("wrong"))
	_, err := c.GetToken(context.Background(), "conn-1")
	var env ErrorEnvelope
	if !errorsAs(err, &env) {
		t.Fatalf("want ErrorEnvelope, got %T: %v", err, err)
	}
	if env.Code != "invalid_api_key" || env.StatusCode != http.StatusForbidden {
		t.Fatalf("got code=%q status=%d", env.Code, env.StatusCode)
	}
}

func errorsAs(err error, target *ErrorEnvelope) bool {
	e, ok := err.(ErrorEnvelope)
	if ok {
		*target = e
	}
	return ok
}
