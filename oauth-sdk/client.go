package oauthsdk

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "bytes"
    "math/rand"
    "net/http"
    "net/url"
    "strings"
    "time"
)

// Client is a thin HTTP client for the Dromos OAuth Gateway.
type Client struct {
    GatewayBaseURL string
    HTTPClient     *http.Client

    // Optional for temporary direct refresh via Broker until gateway has proxy
    BrokerBaseURL string
    BrokerAPIKey  string

    // Optional logger and retry policy
    Logger      Logger
    RetryPolicy RetryPolicy
}

// New creates a new Client with sane defaults.
func New(gatewayBaseURL string, opts ...Option) *Client {
    c := &Client{
        GatewayBaseURL: strings.TrimRight(gatewayBaseURL, "/"),
        HTTPClient: &http.Client{Timeout: 30 * time.Second},
    }
    for _, o := range opts {
        o(c)
    }
    return c
}

type Option func(*Client)

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.HTTPClient = h } }
func WithBroker(baseURL, apiKey string) Option {
    return func(c *Client) {
        c.BrokerBaseURL = strings.TrimRight(baseURL, "/")
        c.BrokerAPIKey = apiKey
    }
}
func WithLogger(l Logger) Option { return func(c *Client) { c.Logger = l } }
func WithRetry(p RetryPolicy) Option { return func(c *Client) { c.RetryPolicy = p } }

// Logger is a minimal logging interface.
type Logger interface {
    Infof(format string, args ...any)
    Errorf(format string, args ...any)
}

// RetryPolicy configures basic retry behavior.
type RetryPolicy struct {
    Retries    int           // total attempts = Retries + 1
    MinDelay   time.Duration // base backoff (e.g., 200ms)
    MaxDelay   time.Duration // cap (e.g., 2s)
    RetryOn429 bool          // also retry on 429
}

func (p RetryPolicy) normalized() RetryPolicy {
    q := p
    if q.Retries < 0 { q.Retries = 0 }
    if q.MinDelay <= 0 { q.MinDelay = 200 * time.Millisecond }
    if q.MaxDelay <= 0 { q.MaxDelay = 2 * time.Second }
    if q.MaxDelay < q.MinDelay { q.MaxDelay = q.MinDelay }
    return q
}

type RequestConnectionInput struct {
    UserID       string   `json:"user_id"`
    ProviderName string   `json:"provider_name"`
    Scopes       []string `json:"scopes"`
    ReturnURL    string   `json:"return_url"`
    Metadata     any      `json:"metadata,omitempty"`
}

type RequestConnectionResponse struct {
    AuthURL      string   `json:"authUrl"`
    ConnectionID string   `json:"connection_id"`
    State        string   `json:"state,omitempty"`
    Scopes       []string `json:"scopes,omitempty"`
    ProviderID   string   `json:"provider_id,omitempty"`
}

type ConnectionStatusResponse struct { Status string `json:"status"` }

// TokenResponse is minimally typed; extra fields are retained in Raw.
type TokenResponse struct {
    AccessToken  string                 `json:"access_token"`
    TokenType    *string                `json:"token_type,omitempty"`
    ExpiresIn    *int64                 `json:"expires_in,omitempty"`
    ExpiresAt    any                    `json:"expires_at,omitempty"`
    Scope        *string                `json:"scope,omitempty"`
    IDToken      *string                `json:"id_token,omitempty"`
    RefreshToken *string                `json:"refresh_token,omitempty"`
    Provider     *string                `json:"provider,omitempty"`
    Strategy     map[string]interface{} `json:"strategy,omitempty"`
    Credentials  map[string]interface{} `json:"credentials,omitempty"`
    Raw          map[string]any         `json:"-"`
}

type ErrorEnvelope struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}

func (e ErrorEnvelope) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// RequestConnection wraps POST /v1/request-connection
func (c *Client) RequestConnection(ctx context.Context, in RequestConnectionInput) (*RequestConnectionResponse, error) {
    body, err := json.Marshal(in)
    if err != nil { return nil, err }
    resp, err := c.do(ctx, http.MethodPost, c.GatewayBaseURL+"/v1/request-connection", map[string]string{"Content-Type": "application/json"}, body)
    if err != nil { return nil, err }
    defer resp.Body.Close()
    var out RequestConnectionResponse
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { return nil, err }
    return &out, nil
}

// CheckConnection wraps GET /v1/check-connection/{connection_id}
func (c *Client) CheckConnection(ctx context.Context, connectionID string) (string, error) {
    if strings.TrimSpace(connectionID) == "" { return "", errors.New("missing connection_id") }
    resp, err := c.do(ctx, http.MethodGet, c.GatewayBaseURL+"/v1/check-connection/"+url.PathEscape(connectionID), nil, nil)
    if err != nil { return "", err }
    defer resp.Body.Close()
    var out ConnectionStatusResponse
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { return "", err }
    return out.Status, nil
}

// GetToken wraps GET /v1/token/{connection_id}
func (c *Client) GetToken(ctx context.Context, connectionID string) (*TokenResponse, error) {
    if strings.TrimSpace(connectionID) == "" { return nil, errors.New("missing connection_id") }
    resp, err := c.do(ctx, http.MethodGet, c.GatewayBaseURL+"/v1/token/"+url.PathEscape(connectionID), nil, nil)
    if err != nil { return nil, err }
    defer resp.Body.Close()
    var raw map[string]any
    if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil { return nil, err }
    tr := &TokenResponse{Raw: raw}
    if v, ok := raw["access_token"].(string); ok { tr.AccessToken = v }
    if v, ok := raw["token_type"].(string); ok { tr.TokenType = &v }
    if v, ok := raw["expires_in"].(float64); ok { vv := int64(v); tr.ExpiresIn = &vv }
    if v, ok := raw["expires_at"]; ok { tr.ExpiresAt = v }
    if v, ok := raw["scope"].(string); ok { tr.Scope = &v }
    if v, ok := raw["id_token"].(string); ok { tr.IDToken = &v }
    if v, ok := raw["refresh_token"].(string); ok { tr.RefreshToken = &v }
    if v, ok := raw["provider"].(string); ok { tr.Provider = &v }
    if v, ok := raw["strategy"].(map[string]interface{}); ok { tr.Strategy = v }
    if v, ok := raw["credentials"].(map[string]interface{}); ok { tr.Credentials = v }
    return tr, nil
}

// RefreshConnection calls the Gateway to force a token refresh.
func (c *Client) RefreshConnection(ctx context.Context, connectionID string) (*TokenResponse, error) {
    if strings.TrimSpace(connectionID) == "" { return nil, errors.New("missing connection_id") }
    resp, err := c.do(ctx, http.MethodPost, c.GatewayBaseURL+"/v1/refresh/"+url.PathEscape(connectionID), nil, nil)
    if err != nil { return nil, err }
    defer resp.Body.Close()
    var raw map[string]any
    if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil { return nil, err }
    tr := &TokenResponse{Raw: raw}
    if v, ok := raw["access_token"].(string); ok { tr.AccessToken = v }
    if v, ok := raw["token_type"].(string); ok { tr.TokenType = &v }
    if v, ok := raw["expires_in"].(float64); ok { vv := int64(v); tr.ExpiresIn = &vv }
    if v, ok := raw["expires_at"]; ok { tr.ExpiresAt = v }
    if v, ok := raw["scope"].(string); ok { tr.Scope = &v }
    if v, ok := raw["id_token"].(string); ok { tr.IDToken = &v }
    if v, ok := raw["refresh_token"].(string); ok { tr.RefreshToken = &v }
    if v, ok := raw["provider"].(string); ok { tr.Provider = &v }
    if v, ok := raw["strategy"].(map[string]interface{}); ok { tr.Strategy = v }
    if v, ok := raw["credentials"].(map[string]interface{}); ok { tr.Credentials = v }
    return tr, nil
}

// RefreshViaBroker calls RefreshConnection (Gateway Proxy).
// Deprecated: Use RefreshConnection instead. This method no longer calls the Broker directly.
func (c *Client) RefreshViaBroker(ctx context.Context, connectionID string) (*TokenResponse, error) {
    return c.RefreshConnection(ctx, connectionID)
}

func readGatewayError(r io.Reader, status int) error {
    var e ErrorEnvelope
    b, _ := io.ReadAll(r)
    if err := json.Unmarshal(b, &e); err == nil && e.Code != "" {
        return e
    }
    if len(b) > 0 { return fmt.Errorf("gateway error %d: %s", status, strings.TrimSpace(string(b))) }
    return fmt.Errorf("gateway error %d", status)
}

// WaitForActive polls check-connection until active/failed or timeout.
func (c *Client) WaitForActive(ctx context.Context, connectionID string, interval time.Duration) (string, error) {
    if interval <= 0 { interval = 1500 * time.Millisecond }
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for {
        status, err := c.CheckConnection(ctx, connectionID)
        if err != nil { return "", err }
        switch status {
        case "active":
            return status, nil
        case "failed":
            return status, nil
        }
        select {
        case <-ctx.Done():
            return "", ctx.Err()
        case <-ticker.C:
        }
    }
}

// do executes an HTTP request with retries according to the policy.
func (c *Client) do(ctx context.Context, method, urlStr string, headers map[string]string, body []byte) (*http.Response, error) {
    // single attempt helper
    attempt := func() (*http.Response, error) {
        var bodyReader io.Reader
        if body != nil { bodyReader = bytes.NewReader(body) }
        req, err := http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
        if err != nil { return nil, err }
        for k, v := range headers {
            req.Header.Set(k, v)
        }
        resp, err := c.HTTPClient.Do(req)
        if err != nil { return nil, err }
        if resp.StatusCode >= 200 && resp.StatusCode < 300 {
            return resp, nil
        }
        // classify retryable statuses
        retryable := resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout
        if c.RetryPolicy.RetryOn429 && resp.StatusCode == http.StatusTooManyRequests {
            retryable = true
        }
        if !retryable {
            // return immediate error with body preserved
            return nil, readGatewayError(resp.Body, resp.StatusCode)
        }
        // drain body before retry
        io.Copy(io.Discard, resp.Body)
        resp.Body.Close()
        return nil, fmt.Errorf("retryable status: %d", resp.StatusCode)
    }

    pol := c.RetryPolicy.normalized()
    var resp *http.Response
    var err error
    for i := 0; i <= pol.Retries; i++ {
        if c.Logger != nil { c.Logger.Infof("http %s %s attempt %d", method, urlStr, i+1) }
        resp, err = attempt()
        if err == nil && resp != nil {
            return resp, nil
        }
        // last attempt
        if i == pol.Retries {
            if err == nil {
                return resp, nil
            }
            return nil, err
        }
        // backoff with jitter
        delay := backoff(i, pol.MinDelay, pol.MaxDelay)
        if c.Logger != nil { c.Logger.Infof("retrying in %s: %v", delay, err) }
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-time.After(delay):
        }
    }
    return resp, err
}

func backoff(attempt int, minDelay, maxDelay time.Duration) time.Duration {
    // exponential with jitter, capped growth
    if attempt < 0 { attempt = 0 }
    if attempt > 10 { attempt = 10 }
    factor := 1 << uint(attempt)
    base := float64(minDelay) * float64(factor)
    if base > float64(maxDelay) { base = float64(maxDelay) }
    jitter := 0.2 + rand.Float64()*0.6 // 0.2..0.8
    return time.Duration(base * jitter)
}


