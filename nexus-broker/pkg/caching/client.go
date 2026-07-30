package caching

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

// cachingTransport is an http.RoundTripper that caches responses in Redis.
type cachingTransport struct {
	redisClient *redis.Client
	transport   http.RoundTripper
	ttl         time.Duration
}

// RoundTrip implements the http.RoundTripper interface.
func (t *cachingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != "GET" {
		return t.transport.RoundTrip(req)
	}

	// Never cache credentialed requests. The cache key below is the URL alone,
	// so an authenticated response would be replayed to any later caller of the
	// same URL regardless of who they authenticated as — both a correctness bug
	// (one principal's credential validating another's) and a disclosure risk
	// (one principal's response body served to another). Only unauthenticated
	// GETs — OIDC discovery documents, JWKS — are safe to share.
	if isCredentialed(req) {
		return t.transport.RoundTrip(req)
	}

	cacheKey := "http:" + req.URL.String()

	// Try to get the response from cache
	cached, err := t.redisClient.Get(req.Context(), cacheKey).Bytes()
	if err == nil {
		// Cache hit
		b := bytes.NewBuffer(cached)
		return http.ReadResponse(bufio.NewReader(b), req)
	}

	// Cache miss, call the real transport
	resp, err := t.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Dump the response to bytes
	dump, err := httputil.DumpResponse(resp, true)
	if err != nil {
		return nil, err
	}

	// Save the response to cache
	err = t.redisClient.Set(req.Context(), cacheKey, dump, t.ttl).Err()
	if err != nil {
		// Log the error but don't fail the request
	}

	// Since DumpResponse consumes the body, we need to create a new one
	resp.Body = io.NopCloser(bytes.NewBuffer(dump))
	// We need to re-read the response to get the body back
	b := bytes.NewBuffer(dump)
	newResp, err := http.ReadResponse(bufio.NewReader(b), req)
	if err != nil {
		return nil, err
	}

	return newResp, nil
}

// isCredentialed reports whether a request carries caller-specific credentials,
// in which case its response must not enter a URL-keyed shared cache.
func isCredentialed(req *http.Request) bool {
	if req.Header.Get("Authorization") != "" || req.Header.Get("Proxy-Authorization") != "" {
		return true
	}
	if len(req.Cookies()) > 0 {
		return true
	}
	// Common non-standard API-key headers used by providers in place of
	// Authorization (e.g. Shortcut-Token, X-TrackerToken, Api-Token, X-API-KEY).
	for name := range req.Header {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "api-key") || strings.Contains(lower, "apikey") ||
			strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
			return true
		}
	}
	return false
}

// NewCachingClient returns a new http.Client configured with the cachingTransport.
func NewCachingClient(redisClient *redis.Client, cacheTTL time.Duration) *http.Client {
	return &http.Client{
		Transport: &cachingTransport{
			redisClient: redisClient,
			transport:   http.DefaultTransport,
			ttl:         cacheTTL,
		},
	}
}
