package sidecar

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

type fakeTokenProvider struct {
	mu     sync.Mutex
	calls  int
	tokens map[string]*oauthsdk.TokenResponse
	err    error
}

func (p *fakeTokenProvider) GetToken(ctx context.Context, connectionID string) (*oauthsdk.TokenResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	if token, ok := p.tokens[connectionID]; ok {
		return token, nil
	}
	return nil, errors.New("token not found")
}

func (p *fakeTokenProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestProxyUsesPathRouteAndOAuthToken(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/repos" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.RawQuery; got != "per_page=1" {
			t.Fatalf("query = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get(connectionIDHeader); got != "" {
			t.Fatalf("sidecar connection header leaked: %q", got)
		}
		if got := r.Header.Get("X-Nexus-Debug"); got != "" {
			t.Fatalf("sidecar debug header leaked: %q", got)
		}
		w.Header().Set("X-Upstream", "ok")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	proxy := newTestProxy(t, upstream.URL, &fakeTokenProvider{tokens: map[string]*oauthsdk.TokenResponse{
		"conn-1": {AccessToken: "secret-token"},
	}})

	req := httptest.NewRequest(http.MethodGet, "/github/user/repos?per_page=1", nil)
	req.Header.Set(connectionIDHeader, "conn-1")
	req.Header.Set("Authorization", "Bearer caller-token")
	req.Header.Set("X-Nexus-Debug", "true")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("X-Upstream"); got != "ok" {
		t.Fatalf("X-Upstream = %q", got)
	}
}

func TestProxyUsesProviderHeaderRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/repos" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	proxy := newTestProxy(t, upstream.URL, &fakeTokenProvider{tokens: map[string]*oauthsdk.TokenResponse{
		"conn-1": {AccessToken: "secret-token"},
	}})

	req := httptest.NewRequest(http.MethodGet, "/user/repos", nil)
	req.Header.Set(providerHeader, "github")
	req.Header.Set(connectionIDHeader, "conn-1")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestProxyRequiresConnectionID(t *testing.T) {
	proxy := newTestProxy(t, "https://api.example.com", &fakeTokenProvider{})

	req := httptest.NewRequest(http.MethodGet, "/github/user", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)
	assertErrorCode(t, rr, http.StatusBadRequest, "missing_connection_id")
}

func TestProxyRejectsUnknownRoute(t *testing.T) {
	proxy := newTestProxy(t, "https://api.example.com", &fakeTokenProvider{})

	req := httptest.NewRequest(http.MethodGet, "/slack/user", nil)
	req.Header.Set(connectionIDHeader, "conn-1")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)
	assertErrorCode(t, rr, http.StatusNotFound, "unknown_route")
}

func TestProxyAppliesStaticStrategies(t *testing.T) {
	tests := []struct {
		name       string
		token      *oauthsdk.TokenResponse
		assertAuth func(t *testing.T, r *http.Request)
	}{
		{
			name: "custom header",
			token: &oauthsdk.TokenResponse{
				Strategy: map[string]interface{}{
					"type": "header",
					"config": map[string]interface{}{
						"header_name":      "X-API-Key",
						"credential_field": "api_key",
					},
				},
				Credentials: map[string]interface{}{"api_key": "key-123"},
			},
			assertAuth: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("X-API-Key"); got != "key-123" {
					t.Fatalf("X-API-Key = %q", got)
				}
			},
		},
		{
			name: "query param",
			token: &oauthsdk.TokenResponse{
				Strategy: map[string]interface{}{
					"type": "query_param",
					"config": map[string]interface{}{
						"param_name":       "api_key",
						"credential_field": "api_key",
					},
				},
				Credentials: map[string]interface{}{"api_key": "key-456"},
			},
			assertAuth: func(t *testing.T, r *http.Request) {
				if got := r.URL.Query().Get("api_key"); got != "key-456" {
					t.Fatalf("api_key query = %q", got)
				}
			},
		},
		{
			name: "basic auth",
			token: &oauthsdk.TokenResponse{
				Strategy:    map[string]interface{}{"type": "basic_auth"},
				Credentials: map[string]interface{}{"username": "user", "password": "pass"},
			},
			assertAuth: func(t *testing.T, r *http.Request) {
				user, pass, ok := r.BasicAuth()
				if !ok || user != "user" || pass != "pass" {
					t.Fatalf("BasicAuth = %q/%q/%v", user, pass, ok)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.assertAuth(t, r)
				_, _ = w.Write([]byte("ok"))
			}))
			defer upstream.Close()

			proxy := newTestProxy(t, upstream.URL, &fakeTokenProvider{tokens: map[string]*oauthsdk.TokenResponse{"conn-1": tt.token}})
			req := httptest.NewRequest(http.MethodGet, "/github/user?existing=1", nil)
			req.Header.Set(connectionIDHeader, "conn-1")
			rr := httptest.NewRecorder()

			proxy.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestProxyAppliesHMACPayloadAndPreservesBody(t *testing.T) {
	body := "signed body"
	secret := "super-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	wantSignature := hex.EncodeToString(mac.Sum(nil))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(gotBody) != body {
			t.Fatalf("body = %q", gotBody)
		}
		if got := r.Header.Get("X-Signature"); got != wantSignature {
			t.Fatalf("signature = %q, want %q", got, wantSignature)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	proxy := newTestProxy(t, upstream.URL, &fakeTokenProvider{tokens: map[string]*oauthsdk.TokenResponse{
		"conn-1": {
			Strategy: map[string]interface{}{
				"type": "hmac_payload",
				"config": map[string]interface{}{
					"header_name":  "X-Signature",
					"secret_field": "api_secret",
					"algo":         "sha256",
					"encoding":     "hex",
				},
			},
			Credentials: map[string]interface{}{"api_secret": secret},
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/github/payload", strings.NewReader(body))
	req.Header.Set(connectionIDHeader, "conn-1")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestProxyCachesTokensUntilExpiry(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	expiresIn := int64(60)
	provider := &fakeTokenProvider{tokens: map[string]*oauthsdk.TokenResponse{
		"conn-1": {AccessToken: "secret-token", ExpiresIn: &expiresIn},
	}}
	proxy := newTestProxy(t, upstream.URL, provider)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/github/user", nil)
		req.Header.Set(connectionIDHeader, "conn-1")
		rr := httptest.NewRecorder()
		proxy.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d status = %d body=%s", i+1, rr.Code, rr.Body.String())
		}
	}

	if got := provider.callCount(); got != 1 {
		t.Fatalf("token provider calls = %d, want 1", got)
	}
}

func TestProxyRejectsBodiesAboveLimit(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	proxy := newTestProxyWithLimit(t, upstream.URL, &fakeTokenProvider{tokens: map[string]*oauthsdk.TokenResponse{
		"conn-1": {AccessToken: "secret-token"},
	}}, 4)

	req := httptest.NewRequest(http.MethodPost, "/github/user", strings.NewReader("12345"))
	req.Header.Set(connectionIDHeader, "conn-1")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)
	assertErrorCode(t, rr, http.StatusBadGateway, "auth_injection_failed")
	if upstreamCalled {
		t.Fatal("upstream was called")
	}
}

func TestHealthEndpoint(t *testing.T) {
	proxy := newTestProxy(t, "https://api.example.com", &fakeTokenProvider{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	assertJSONField(t, rr.Body.String(), "status", "healthy")
}

func newTestProxy(t *testing.T, target string, provider *fakeTokenProvider) *Proxy {
	t.Helper()
	return newTestProxyWithLimit(t, target, provider, 10*1024*1024)
}

func newTestProxyWithLimit(t *testing.T, target string, provider *fakeTokenProvider, bodyLimit int64) *Proxy {
	t.Helper()
	targetURL, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	proxy, err := NewProxy(Config{
		Routes: map[string]Route{
			"github": {Name: "github", Target: targetURL},
		},
		TokenProvider:    provider,
		RequestBodyLimit: bodyLimit,
		TokenCacheTTL:    time.Minute,
	})
	if err != nil {
		t.Fatalf("NewProxy returned error: %v", err)
	}
	return proxy
}

func assertErrorCode(t *testing.T, rr *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rr.Code != status {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	assertJSONField(t, rr.Body.String(), "error", code)
}

func assertJSONField(t *testing.T, raw, field, want string) {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decode response %q: %v", raw, err)
	}
	if got := body[field]; got != want {
		t.Fatalf("%s = %q, want %q", field, got, want)
	}
}
