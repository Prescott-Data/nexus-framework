package caching

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
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
