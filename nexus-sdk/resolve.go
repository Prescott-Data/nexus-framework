package oauthsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ──────────────────────────────────────────────
//  MCP / Workspace-Scoped Token Resolution
// ──────────────────────────────────────────────

// ResolveTokenResponse is the parsed response from GET /v1/resolve.
type ResolveTokenResponse struct {
	AccessToken string                 `json:"access_token"`
	TokenType   string                 `json:"token_type"`
	ExpiresAt   *string                `json:"expires_at,omitempty"`
	Strategy    map[string]interface{} `json:"strategy,omitempty"`
	Credentials map[string]interface{} `json:"credentials,omitempty"`
}

// ResolveToken fetches a token from the Gateway using workspace ID + provider name.
// This is the primary method used by MCP servers for dynamic, workspace-scoped auth.
// Wraps GET /v1/resolve?workspace_id=...&provider_name=...
func (c *Client) ResolveToken(ctx context.Context, workspaceID, providerName string) (*ResolveTokenResponse, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, fmt.Errorf("missing workspace_id")
	}
	if strings.TrimSpace(providerName) == "" {
		return nil, fmt.Errorf("missing provider_name")
	}

	u := fmt.Sprintf("%s/v1/resolve?workspace_id=%s&provider_name=%s",
		c.GatewayBaseURL,
		url.QueryEscape(workspaceID),
		url.QueryEscape(providerName),
	)

	resp, err := c.do(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Parse the full response into a raw map first
	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("resolve: decode error: %w", err)
	}

	out := &ResolveTokenResponse{}

	// Extract credentials block
	if creds, ok := raw["credentials"].(map[string]interface{}); ok {
		out.Credentials = creds
		if at, ok := creds["access_token"].(string); ok {
			out.AccessToken = at
		}
		if tt, ok := creds["token_type"].(string); ok {
			out.TokenType = tt
		}
		if ea, ok := creds["expires_at"].(string); ok {
			out.ExpiresAt = &ea
		}
	}

	// Fallback to top-level fields
	if out.AccessToken == "" {
		if at, ok := raw["access_token"].(string); ok {
			out.AccessToken = at
		}
	}
	if out.TokenType == "" {
		if tt, ok := raw["token_type"].(string); ok {
			out.TokenType = tt
		}
	}
	if out.ExpiresAt == nil {
		if ea, ok := raw["expires_at"].(string); ok {
			out.ExpiresAt = &ea
		}
	}

	// Extract strategy
	if strat, ok := raw["strategy"].(map[string]interface{}); ok {
		out.Strategy = strat
	}

	// Default token type
	if out.TokenType == "" {
		out.TokenType = "Bearer"
	}

	if out.AccessToken == "" {
		return nil, fmt.Errorf("resolve: no access_token found in gateway response")
	}

	return out, nil
}

// GetCachedToken resolves a token from the cache or fetches a fresh one from the gateway.
// This is the high-level method that MCP servers should use.
func (c *Client) GetCachedToken(ctx context.Context, cache *TokenCache, workspaceID, providerName string) (*CachedToken, error) {
	if cache == nil {
		return nil, fmt.Errorf("token cache is nil")
	}

	// Check cache first
	if cached := cache.Get(workspaceID, providerName); cached != nil {
		if c.Logger != nil {
			c.Logger.Infof("using cached token for workspace=%s provider=%s", workspaceID, providerName)
		}
		return cached, nil
	}

	// Fetch fresh
	if c.Logger != nil {
		c.Logger.Infof("resolving fresh token for workspace=%s provider=%s", workspaceID, providerName)
	}

	resolved, err := c.ResolveToken(ctx, workspaceID, providerName)
	if err != nil {
		return nil, err
	}

	// Parse expiration — conservative 5-minute default
	expiresAt := time.Now().Add(5 * time.Minute)
	if resolved.ExpiresAt != nil {
		if parsed, err := time.Parse(time.RFC3339, *resolved.ExpiresAt); err == nil {
			expiresAt = parsed
		} else if c.Logger != nil {
			c.Logger.Infof("could not parse expires_at=%q, using 5-minute fallback", *resolved.ExpiresAt)
		}
	} else if c.Logger != nil {
		c.Logger.Infof("no expires_at in response for workspace=%s provider=%s, using 5-minute fallback", workspaceID, providerName)
	}

	token := CachedToken{
		AccessToken: resolved.AccessToken,
		TokenType:   resolved.TokenType,
		ExpiresAt:   expiresAt,
	}

	cache.Set(workspaceID, providerName, token)
	return &token, nil
}

// ──────────────────────────────────────────────
//  Authenticated HTTP Transport (Go equivalent of createFetcher)
// ──────────────────────────────────────────────

// AuthenticatedTransport is an http.RoundTripper that automatically injects
// Nexus authentication headers into every outgoing request.
//
// This is the Go equivalent of the TypeScript SDK's createFetcher().
//
// Usage:
//
//	client := oauthsdk.New("https://gateway.example.com")
//	cache := oauthsdk.NewTokenCache(30 * time.Second)
//
//	httpClient := &http.Client{
//	    Transport: client.AuthenticatedTransport(cache, "ws-001", "github"),
//	}
//
//	// All requests through this httpClient will have Authorization headers injected.
//	resp, err := httpClient.Get("https://api.github.com/user/repos")
type AuthenticatedTransport struct {
	client      *Client
	cache       *TokenCache
	workspaceID string
	provider    string
	base        http.RoundTripper
}

// NewAuthenticatedTransport creates an http.RoundTripper that injects Nexus auth headers.
// If base is nil, http.DefaultTransport is used.
func (c *Client) NewAuthenticatedTransport(cache *TokenCache, workspaceID, provider string, base http.RoundTripper) *AuthenticatedTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &AuthenticatedTransport{
		client:      c,
		cache:       cache,
		workspaceID: workspaceID,
		provider:    provider,
		base:        base,
	}
}

// RoundTrip implements the http.RoundTripper interface.
// It resolves a valid token and injects the Authorization header before delegating to the base transport.
func (t *AuthenticatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.client.GetCachedToken(req.Context(), t.cache, t.workspaceID, t.provider)
	if err != nil {
		return nil, fmt.Errorf("nexus-sdk: failed to resolve token: %w", err)
	}

	// Clone the request to avoid mutating the original
	clone := req.Clone(req.Context())

	// Normalize token type per RFC 6750
	tokenType := token.TokenType
	if strings.EqualFold(tokenType, "bearer") {
		tokenType = "Bearer"
	}

	clone.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, token.AccessToken))

	return t.base.RoundTrip(clone)
}

// AuthenticatedHTTPClient is a convenience method that returns an *http.Client
// with the AuthenticatedTransport pre-configured.
//
// Usage:
//
//	httpClient := nexusClient.AuthenticatedHTTPClient(cache, "ws-001", "github")
//	resp, err := httpClient.Get("https://api.github.com/user/repos")
func (c *Client) AuthenticatedHTTPClient(cache *TokenCache, workspaceID, provider string) *http.Client {
	return &http.Client{
		Transport: c.NewAuthenticatedTransport(cache, workspaceID, provider, nil),
		Timeout:   30 * time.Second,
	}
}
