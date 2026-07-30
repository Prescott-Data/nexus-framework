package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/caching"
)

// A credential-validation probe must reach the provider on every attempt.
//
// Regression test for a fail-open found during end-to-end testing: the broker
// passed its shared caching client (main.go -> NewConnectionService) to
// validateCredentials. cachingTransport keys responses on the request URL alone,
// so the Authorization header was invisible to the cache. Once any valid
// credential had been validated against an endpoint, every subsequent probe for
// that URL was served the cached 200 — accepting arbitrary credentials for the
// full cache TTL — and a cached 401 conversely rejected valid ones.
func TestValidateCredentials_ProbeIsNeverCached(t *testing.T) {
	var probes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes++
		user, pass, _ := r.BasicAuth()
		if user == "admin" && pass == "good" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	params := rawParams(t, map[string]interface{}{
		"auth_strategy": map[string]interface{}{
			"type":   "basic_auth",
			"config": map[string]interface{}{"username_field": "username", "password_field": "password"},
		},
	})

	svc := &connectionService{probeClient: srv.Client()}

	good := map[string]interface{}{"username": "admin", "password": "good"}
	if err := svc.validateCredentials(context.Background(), "basic_auth", "", srv.URL, "/me", params, good); err != nil {
		t.Fatalf("valid credential should pass: %v", err)
	}

	// Same URL, different credential. This must be probed again, not served
	// from a previous result.
	bad := map[string]interface{}{"username": "admin", "password": "WRONG"}
	if err := svc.validateCredentials(context.Background(), "basic_auth", "", srv.URL, "/me", params, bad); err == nil {
		t.Fatal("invalid credential was accepted — validation result was reused across credentials")
	}

	if probes != 2 {
		t.Fatalf("expected the provider to be probed once per attempt, got %d probes", probes)
	}
}

// The shared caching transport must not store responses to credentialed
// requests at all: the key is URL-only, so a cached authenticated response
// would be replayed to any other caller of the same URL.
func TestCachingTransport_DoesNotCacheCredentialedRequests(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		user, pass, _ := r.BasicAuth()
		if (user == "admin" && pass == "good") || r.Header.Get("X-Api-Key") == "good" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	// A caching client with no Redis behind it: every cache read errors, which
	// exercises the miss path. What matters is that a credentialed request is
	// routed straight to the underlying transport and never written to cache —
	// a nil Redis client would panic on Set if the guard were missing.
	client := caching.NewCachingClient(nil, 0)

	for _, tc := range []struct {
		name  string
		apply func(r *http.Request)
	}{
		{"basic auth", func(r *http.Request) { r.SetBasicAuth("admin", "good") }},
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer good") }},
		{"custom api key header", func(r *http.Request) { r.Header.Set("X-Api-Key", "good") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := hits
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/me", nil)
			tc.apply(req)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("credentialed request should pass through: %v", err)
			}
			defer resp.Body.Close()
			if hits != before+1 {
				t.Fatal("credentialed request did not reach the origin")
			}
		})
	}
}
