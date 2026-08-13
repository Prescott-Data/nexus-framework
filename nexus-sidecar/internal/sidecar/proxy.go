package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	bridgeauth "github.com/Prescott-Data/nexus-framework/nexus-bridge/pkg/auth"
	oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	connectionIDHeader = "X-Nexus-Connection-ID"
	providerHeader     = "X-Nexus-Provider"
)

type Route struct {
	Name   string
	Target *url.URL
}

type TokenProvider interface {
	GetToken(ctx context.Context, connectionID string) (*oauthsdk.TokenResponse, error)
}

type Config struct {
	Routes           map[string]Route
	TokenProvider    TokenProvider
	HTTPClient       *http.Client
	TokenCacheTTL    time.Duration
	RequestBodyLimit int64
}

type Proxy struct {
	routes           map[string]Route
	tokenProvider    TokenProvider
	transport        http.RoundTripper
	tokenCache       *tokenCache
	requestBodyLimit int64
	requestsTotal    *prometheus.CounterVec
}

func NewProxy(cfg Config) (*Proxy, error) {
	if len(cfg.Routes) == 0 {
		return nil, errors.New("at least one route is required")
	}
	if cfg.TokenProvider == nil {
		return nil, errors.New("token provider is required")
	}

	transport := http.DefaultTransport
	if cfg.HTTPClient != nil && cfg.HTTPClient.Transport != nil {
		transport = cfg.HTTPClient.Transport
	}

	limit := cfg.RequestBodyLimit
	if limit <= 0 {
		limit = 10 * 1024 * 1024
	}

	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nexus_sidecar_proxy_requests_total",
		Help: "Total sidecar proxy requests by route, status, and outcome.",
	}, []string{"route", "status", "outcome"})
	if err := prometheus.Register(requests); err != nil {
		if existing, ok := err.(prometheus.AlreadyRegisteredError); ok {
			collector, ok := existing.ExistingCollector.(*prometheus.CounterVec)
			if !ok {
				return nil, err
			}
			requests = collector
		} else {
			return nil, err
		}
	}

	return &Proxy{
		routes:           cloneRoutes(cfg.Routes),
		tokenProvider:    cfg.TokenProvider,
		transport:        transport,
		tokenCache:       newTokenCache(cfg.TokenCacheTTL),
		requestBodyLimit: limit,
		requestsTotal:    requests,
	}, nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
		return
	}

	route, rest, ok := p.matchRoute(r)
	if !ok {
		p.writeProxyError(w, "", http.StatusNotFound, "unknown_route", "unknown sidecar route")
		return
	}

	connectionID := strings.TrimSpace(r.Header.Get(connectionIDHeader))
	if connectionID == "" {
		p.writeProxyError(w, route.Name, http.StatusBadRequest, "missing_connection_id", "missing X-Nexus-Connection-ID")
		return
	}

	token, err := p.getToken(r.Context(), connectionID)
	if err != nil {
		log.Printf("sidecar token fetch failed route=%s connection_id=%s error=%v", route.Name, connectionID, err)
		p.writeProxyError(w, route.Name, http.StatusBadGateway, "token_fetch_failed", "failed to fetch Nexus credentials")
		return
	}

	outbound, err := p.prepareOutboundRequest(r, route, rest, token)
	if err != nil {
		log.Printf("sidecar auth injection failed route=%s connection_id=%s error=%v", route.Name, connectionID, err)
		p.writeProxyError(w, route.Name, http.StatusBadGateway, "auth_injection_failed", "failed to apply Nexus credentials")
		return
	}

	proxy := &httputil.ReverseProxy{
		Director:  func(req *http.Request) {},
		Transport: p.transport,
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			log.Printf("sidecar proxy error route=%s error=%v", route.Name, err)
			p.writeProxyError(rw, route.Name, http.StatusBadGateway, "upstream_error", "upstream request failed")
		},
		ModifyResponse: func(resp *http.Response) error {
			p.requestsTotal.WithLabelValues(route.Name, fmt.Sprint(resp.StatusCode), "success").Inc()
			removeHopByHopHeaders(resp.Header)
			return nil
		},
	}
	proxy.ServeHTTP(w, outbound)
}

func (p *Proxy) prepareOutboundRequest(r *http.Request, route Route, rest string, token *oauthsdk.TokenResponse) (*http.Request, error) {
	outbound := r.Clone(r.Context())
	outbound.URL = cloneURL(r.URL)
	outbound.Header = r.Header.Clone()
	rewriteRequest(outbound, route.Target, rest, r.URL.RawQuery)
	sanitizeRequestHeaders(outbound.Header)
	outbound.Host = route.Target.Host

	if err := applyToken(outbound, token, p.requestBodyLimit); err != nil {
		return nil, err
	}
	return outbound, nil
}

func (p *Proxy) matchRoute(r *http.Request) (Route, string, bool) {
	if provider := strings.TrimSpace(r.Header.Get(providerHeader)); provider != "" {
		route, ok := p.routes[provider]
		if !ok {
			return Route{}, "", false
		}
		return route, r.URL.Path, true
	}

	clean := strings.TrimPrefix(r.URL.Path, "/")
	name, rest, found := strings.Cut(clean, "/")
	if !found {
		rest = ""
	}
	route, ok := p.routes[name]
	if !ok {
		return Route{}, "", false
	}
	return route, "/" + rest, true
}

func (p *Proxy) getToken(ctx context.Context, connectionID string) (*oauthsdk.TokenResponse, error) {
	if token, ok := p.tokenCache.Get(connectionID); ok {
		return token, nil
	}
	token, err := p.tokenProvider.GetToken(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	p.tokenCache.Set(connectionID, token)
	return token, nil
}

func (p *Proxy) writeProxyError(w http.ResponseWriter, route string, status int, code, message string) {
	if route == "" {
		route = "unknown"
	}
	p.requestsTotal.WithLabelValues(route, fmt.Sprint(status), code).Inc()
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func applyToken(req *http.Request, token *oauthsdk.TokenResponse, bodyLimit int64) error {
	if token == nil {
		return errors.New("empty token response")
	}
	strategy := bridgeauth.AuthStrategy{Type: "oauth2"}
	if token.Strategy != nil {
		strategy = bridgeauth.AuthStrategy{
			Type:   stringFromMap(token.Strategy, "type", "oauth2"),
			Config: mapFromMap(token.Strategy, "config"),
		}
	}

	creds := bridgeauth.Credentials{}
	if token.Credentials != nil {
		for k, v := range token.Credentials {
			creds[k] = v
		}
	}
	for k, v := range token.Raw {
		if _, exists := creds[k]; !exists {
			creds[k] = v
		}
	}
	if token.AccessToken != "" {
		creds["access_token"] = token.AccessToken
	}

	if req.Body != nil {
		body, err := readBodyWithinLimit(req.Body, bodyLimit)
		if err != nil {
			return err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}

	return bridgeauth.ApplyAuthentication(req, strategy, creds)
}

func readBodyWithinLimit(body io.ReadCloser, limit int64) ([]byte, error) {
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("request body exceeds %d byte limit", limit)
	}
	return data, nil
}

func rewriteRequest(req *http.Request, target *url.URL, path, rawQuery string) {
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.URL.Path = singleJoiningSlash(target.Path, path)
	req.URL.RawPath = ""
	if target.RawQuery == "" || rawQuery == "" {
		req.URL.RawQuery = target.RawQuery + rawQuery
	} else {
		req.URL.RawQuery = target.RawQuery + "&" + rawQuery
	}
}

func sanitizeRequestHeaders(header http.Header) {
	removeHopByHopHeaders(header)
	header.Del("Authorization")
	header.Del(connectionIDHeader)
	header.Del(providerHeader)
	for key := range header {
		if strings.HasPrefix(strings.ToLower(key), "x-nexus-") {
			header.Del(key)
		}
	}
}

func removeHopByHopHeaders(header http.Header) {
	for _, h := range strings.Split(header.Get("Connection"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			header.Del(h)
		}
	}
	for _, h := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(h)
	}
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}

func stringFromMap(values map[string]interface{}, key, fallback string) string {
	if v, ok := values[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func mapFromMap(values map[string]interface{}, key string) map[string]interface{} {
	if v, ok := values[key].(map[string]interface{}); ok {
		return v
	}
	if v, ok := values[key].(map[string]any); ok {
		return v
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	data, err := json.Marshal(body)
	if err != nil {
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func cloneRoutes(routes map[string]Route) map[string]Route {
	cloned := make(map[string]Route, len(routes))
	for k, v := range routes {
		copyURL := *v.Target
		cloned[k] = Route{Name: v.Name, Target: &copyURL}
	}
	return cloned
}

func cloneURL(u *url.URL) *url.URL {
	copyURL := *u
	return &copyURL
}

type tokenCache struct {
	ttl     time.Duration
	now     func() time.Time
	mu      sync.RWMutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	token     *oauthsdk.TokenResponse
	expiresAt time.Time
}

func newTokenCache(defaultTTL time.Duration) *tokenCache {
	return &tokenCache{
		ttl:     defaultTTL,
		now:     time.Now,
		entries: make(map[string]cacheEntry),
	}
}

func (c *tokenCache) Get(connectionID string) (*oauthsdk.TokenResponse, bool) {
	c.mu.RLock()
	entry, ok := c.entries[connectionID]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if !entry.expiresAt.IsZero() && !entry.expiresAt.After(c.now()) {
		c.mu.Lock()
		delete(c.entries, connectionID)
		c.mu.Unlock()
		return nil, false
	}
	return entry.token, true
}

func (c *tokenCache) Set(connectionID string, token *oauthsdk.TokenResponse) {
	expiresAt := tokenExpiry(token, c.now(), c.ttl)
	if expiresAt.IsZero() {
		return
	}
	c.mu.Lock()
	c.entries[connectionID] = cacheEntry{token: token, expiresAt: expiresAt}
	c.mu.Unlock()
}

func tokenExpiry(token *oauthsdk.TokenResponse, now time.Time, fallback time.Duration) time.Time {
	if token == nil {
		return time.Time{}
	}
	if token.ExpiresAt != nil {
		switch v := token.ExpiresAt.(type) {
		case string:
			if t, err := time.Parse(time.RFC3339, v); err == nil && t.After(now) {
				return t
			}
		}
	}
	if token.ExpiresIn != nil && *token.ExpiresIn > 0 {
		return now.Add(time.Duration(*token.ExpiresIn) * time.Second)
	}
	if fallback > 0 {
		return now.Add(fallback)
	}
	return time.Time{}
}
