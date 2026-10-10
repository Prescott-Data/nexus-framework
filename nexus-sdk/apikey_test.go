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

	c := NewClient(WithBroker(srv.URL, "agent-key"))
	resp, err := c.do(context.Background(), http.MethodGet, srv.URL+"/v1/providers", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got != "agent-key" {
		t.Fatalf("X-API-Key = %q, want agent-key", got)
	}
}
