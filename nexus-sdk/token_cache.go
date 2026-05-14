package oauthsdk

import (
	"sync"
	"time"
)

// CachedToken holds a resolved token with its expiration metadata.
type CachedToken struct {
	AccessToken string
	TokenType   string
	ExpiresAt   time.Time

	// HeaderName is the HTTP header to set (e.g., "Authorization", "X-API-Key").
	// Defaults to "Authorization" for OAuth2 strategies.
	HeaderName string

	// ValuePrefix is prepended to the token value (e.g., "Bearer ").
	// Empty string for raw API-key strategies.
	ValuePrefix string
}

// TokenCache is a thread-safe, TTL-aware in-memory cache for resolved tokens.
// It is keyed by workspace+provider pairs and automatically evicts expired entries.
type TokenCache struct {
	mu      sync.RWMutex
	entries map[string]CachedToken
	buffer  time.Duration // safety buffer before actual expiry
}

// NewTokenCache creates a new TokenCache.
// buffer is the safety margin subtracted from the expiry time (e.g., 30s)
// so the caller re-fetches slightly before the token actually expires.
func NewTokenCache(buffer time.Duration) *TokenCache {
	if buffer <= 0 {
		buffer = 30 * time.Second
	}
	return &TokenCache{
		entries: make(map[string]CachedToken),
		buffer:  buffer,
	}
}

func cacheKey(workspaceID, provider string) string {
	return workspaceID + ":" + provider
}

// Get returns a cached token if it exists and has not expired.
// Returns nil if the token is missing or stale.
func (tc *TokenCache) Get(workspaceID, provider string) *CachedToken {
	tc.mu.RLock()
	entry, ok := tc.entries[cacheKey(workspaceID, provider)]
	tc.mu.RUnlock()
	if !ok {
		return nil
	}
	if time.Now().After(entry.ExpiresAt.Add(-tc.buffer)) {
		// Token is expired or within the safety buffer — evict and return nil.
		tc.Delete(workspaceID, provider)
		return nil
	}
	return &entry
}

// Set stores a token in the cache.
func (tc *TokenCache) Set(workspaceID, provider string, token CachedToken) {
	tc.mu.Lock()
	tc.entries[cacheKey(workspaceID, provider)] = token
	tc.mu.Unlock()
}

// Delete removes a cached token.
func (tc *TokenCache) Delete(workspaceID, provider string) {
	tc.mu.Lock()
	delete(tc.entries, cacheKey(workspaceID, provider))
	tc.mu.Unlock()
}
