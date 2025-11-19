package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"dromos-oauth-gateway/internal/logging"
)

// Structured error codes for HTTP responses
var (
	ErrInvalidJSON           = errors.New("invalid_json")
	ErrMissingFields         = errors.New("missing_fields")
	ErrInvalidState          = errors.New("invalid_state")
	ErrBrokerUnavailable     = errors.New("broker_unavailable")
	ErrBrokerInvalidResponse = errors.New("broker_invalid_response")
	ErrProviderNotFound      = errors.New("provider_not_found")
	ErrProviderAmbiguous     = errors.New("provider_ambiguous")
)

type BrokerStatusError struct{ Status int }

func (e *BrokerStatusError) Error() string { return fmt.Sprintf("broker status %d", e.Status) }

// writeJSON encodes v as JSON with status
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a structured error body
func writeError(w http.ResponseWriter, status int, code, message string, fields map[string]any) {
	body := map[string]any{
		"error":   code,
		"message": message,
	}
	for k, v := range fields {
		body[k] = v
	}
	writeJSON(w, status, body)
}

type Handler struct {
	brokerBaseURL string
	stateKey      []byte
	httpClient    *http.Client
	providerCache map[string]providerCacheEntry
	cacheMu       sync.RWMutex
	brokerAPIKey  string
}

type providerCacheEntry struct {
	providerID string
	expiresAt  time.Time
}

func NewHandler(brokerBaseURL string, stateKey []byte, httpClient *http.Client) *Handler {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Handler{
		brokerBaseURL: strings.TrimRight(brokerBaseURL, "/"),
		stateKey:      stateKey,
		httpClient:    httpClient,
		providerCache: make(map[string]providerCacheEntry),
		brokerAPIKey:  strings.TrimSpace(getEnv("BROKER_API_KEY", "")),
	}
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// requestConnectionRequest is input for initiating a connection
type requestConnectionRequest struct {
	UserID       string   `json:"user_id"`
	ProviderID   string   `json:"provider_id,omitempty"`
	ProviderName string   `json:"provider_name,omitempty"`
	Scopes       []string `json:"scopes"`
	ReturnURL    string   `json:"return_url"`
	Action       string   `json:"action"`
}

// requestConnectionResponse mirrors broker consentSpec plus connection_id
type requestConnectionResponse struct {
	AuthURL      string   `json:"authUrl"`
	State        string   `json:"state"`
	Scopes       []string `json:"scopes"`
	ProviderID   string   `json:"provider_id"`
	ConnectionID string   `json:"connection_id"`
}

// Core I/O types for reuse in HTTP and gRPC

type RequestConnectionInput struct {
	UserID       string
	ProviderID   string
	ProviderName string
	Scopes       []string
	ReturnURL    string
	Action       string
}

type RequestConnectionOutput struct {
	AuthURL      string
	State        string
	Scopes       []string
	ProviderID   string
	ConnectionID string
}

// RequestConnectionCore performs the broker call and state validation.
func (h *Handler) RequestConnectionCore(ctx context.Context, in RequestConnectionInput) (RequestConnectionOutput, error) {
	logging.Info(ctx, "request_connection.core_start", map[string]any{
		"provider_id":   in.ProviderID,
		"provider_name": in.ProviderName,
		"scopes":        in.Scopes,
		"return_url":    in.ReturnURL,
		"user_id":       in.UserID,
	})

	// Resolve provider_id when only provider_name is provided
	providerID := strings.TrimSpace(in.ProviderID)
	if providerID == "" {
		if strings.TrimSpace(in.ProviderName) == "" {
			return RequestConnectionOutput{}, fmt.Errorf("%w: provider_id or provider_name is required", ErrMissingFields)
		}
		id, err := h.resolveProviderID(ctx, in.ProviderName)
		if err != nil {
			return RequestConnectionOutput{}, err
		}
		providerID = id
	}

	// Azure guidance log (non-mutating)
	if strings.Contains(strings.ToLower(strings.TrimSpace(in.ProviderName)), "azure") || strings.Contains(strings.ToLower(in.ProviderID), "azure") {
		baseOnly := true
		for _, s := range in.Scopes {
			ls := strings.ToLower(strings.TrimSpace(s))
			if ls != "openid" && ls != "email" && ls != "profile" && ls != "offline_access" {
				baseOnly = false
				break
			}
		}
		if baseOnly {
			logging.Info(ctx, "azure_scopes.missing_resource_scope", map[string]any{
				"hint":   "Add a resource scope like User.Read for Azure v2",
				"scopes": in.Scopes,
			})
		}
	}

	payload := map[string]interface{}{
		"workspace_id": in.UserID,
		"provider_id":  providerID,
		"scopes":       in.Scopes,
		"return_url":   in.ReturnURL,
	}
	body, _ := json.Marshal(payload)
	brokerURL := h.brokerBaseURL + "/auth/consent-spec"
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", brokerURL, strings.NewReader(string(body)))
	httpReq.Header.Set("Content-Type", "application/json")
	if h.brokerAPIKey != "" {
		httpReq.Header.Set("X-API-Key", h.brokerAPIKey)
	}

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		logging.Error(ctx, "request_connection.core_broker_error", map[string]any{"error": err.Error()})
		return RequestConnectionOutput{}, fmt.Errorf("%w: %v", ErrBrokerUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logging.Error(ctx, "request_connection.core_broker_status", map[string]any{"status": resp.StatusCode})
		return RequestConnectionOutput{}, &BrokerStatusError{Status: resp.StatusCode}
	}

	var spec struct {
		AuthURL    string   `json:"authUrl"`
		State      string   `json:"state"`
		Scopes     []string `json:"scopes"`
		ProviderID string   `json:"provider_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		logging.Error(ctx, "request_connection.core_decode_error", map[string]any{"error": err.Error()})
		return RequestConnectionOutput{}, fmt.Errorf("%w: %v", ErrBrokerInvalidResponse, err)
	}

	connectionID, err := VerifyAndExtractConnectionID(h.stateKey, spec.State)
	if err != nil {
		logging.Error(ctx, "request_connection.core_state_invalid", map[string]any{"error": err.Error()})
		return RequestConnectionOutput{}, fmt.Errorf("%w: %v", ErrInvalidState, err)
	}

	out := RequestConnectionOutput{
		AuthURL:      spec.AuthURL,
		State:        spec.State,
		Scopes:       spec.Scopes,
		ProviderID:   spec.ProviderID,
		ConnectionID: connectionID,
	}
	logging.Info(ctx, "request_connection.core_success", map[string]any{
		"provider_id":   spec.ProviderID,
		"connection_id": connectionID,
		"auth_url":      logging.RedactQuery(spec.AuthURL),
	})
	return out, nil
}

// resolveProviderID looks up the provider_id by a human-friendly provider name via the broker.
func (h *Handler) resolveProviderID(ctx context.Context, providerName string) (string, error) {
	name := strings.TrimSpace(providerName)
	if name == "" {
		return "", fmt.Errorf("empty provider_name")
	}
	// Try a canonical by-name endpoint first
	byNameURL := h.brokerBaseURL + "/providers/by-name/" + url.PathEscape(name)
	req, _ := http.NewRequestWithContext(ctx, "GET", byNameURL, nil)
	if h.brokerAPIKey != "" {
		req.Header.Set("X-API-Key", h.brokerAPIKey)
	}
	resp, err := h.httpClient.Do(req)
	if err == nil && resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var body struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && strings.TrimSpace(body.ID) != "" {
				return body.ID, nil
			}
		}
	}

	// Fallback: list and filter
	listURL := h.brokerBaseURL + "/providers"
	req2, _ := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	if h.brokerAPIKey != "" {
		req2.Header.Set("X-API-Key", h.brokerAPIKey)
	}
	resp2, err2 := h.httpClient.Do(req2)
	if err2 != nil {
		return "", fmt.Errorf("%w: %v", ErrBrokerUnavailable, err2)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		return "", &BrokerStatusError{Status: resp2.StatusCode}
	}
	var providers []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&providers); err != nil {
		return "", fmt.Errorf("%w: %v", ErrBrokerInvalidResponse, err)
	}
	lower := strings.ToLower(name)
	matches := make([]string, 0, 1)
	for _, p := range providers {
		if strings.ToLower(strings.TrimSpace(p.Name)) == lower {
			matches = append(matches, strings.TrimSpace(p.ID))
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("%w: %s", ErrProviderAmbiguous, name)
	}
	if matches[0] == "" {
		return "", fmt.Errorf("provider id empty for '%s'", name)
	}
	return matches[0], nil
}

// CheckConnectionCore probes broker token endpoint to infer status.
func (h *Handler) CheckConnectionCore(ctx context.Context, connectionID string) (string, error) {
	brokerURL := h.brokerBaseURL + "/connections/" + connectionID + "/token"
	httpReq, _ := http.NewRequestWithContext(ctx, "GET", brokerURL, nil)
	if h.brokerAPIKey != "" {
		httpReq.Header.Set("X-API-Key", h.brokerAPIKey)
	}
	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("broker request failed: %w", err)
	}
	defer resp.Body.Close()

	status := "pending"
	if resp.StatusCode == http.StatusOK {
		status = "active"
	} else if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		status = "failed"
	}
	return status, nil
}

func (h *Handler) RequestConnection(w http.ResponseWriter, r *http.Request) {
	var req requestConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	if len(req.Scopes) == 0 || req.ReturnURL == "" || (req.ProviderID == "" && req.ProviderName == "") {
		writeError(w, http.StatusBadRequest, "missing_fields", "scopes, return_url, and provider are required", nil)
		return
	}

	outCore, err := h.RequestConnectionCore(r.Context(), RequestConnectionInput{
		UserID:       req.UserID,
		ProviderID:   req.ProviderID,
		ProviderName: req.ProviderName,
		Scopes:       req.Scopes,
		ReturnURL:    req.ReturnURL,
		Action:       req.Action,
	})
	if err != nil {
		// Map error types to HTTP statuses
		var be *BrokerStatusError
		switch {
		case errors.Is(err, ErrInvalidState):
			writeError(w, http.StatusBadRequest, "invalid_state", "state verification failed", nil)
			return
		case errors.Is(err, ErrProviderNotFound):
			writeError(w, http.StatusNotFound, "provider_not_found", "provider not found", map[string]any{"provider_name": req.ProviderName})
			return
		case errors.Is(err, ErrProviderAmbiguous):
			writeError(w, http.StatusConflict, "provider_ambiguous", "multiple providers matched", map[string]any{"provider_name": req.ProviderName})
			return
		case errors.As(err, &be):
			writeError(w, http.StatusBadGateway, "broker_error", fmt.Sprintf("broker returned status %d", be.Status), map[string]any{"status": be.Status})
			return
		case errors.Is(err, ErrBrokerUnavailable):
			writeError(w, http.StatusBadGateway, "broker_unavailable", "broker request failed", nil)
			return
		case errors.Is(err, ErrBrokerInvalidResponse):
			writeError(w, http.StatusBadGateway, "broker_invalid_response", "invalid broker response", nil)
			return
		default:
			writeError(w, http.StatusBadGateway, "upstream_error", err.Error(), nil)
			return
		}
	}

	out := requestConnectionResponse{
		AuthURL:      outCore.AuthURL,
		State:        outCore.State,
		Scopes:       outCore.Scopes,
		ProviderID:   outCore.ProviderID,
		ConnectionID: outCore.ConnectionID,
	}

	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) CheckConnection(w http.ResponseWriter, r *http.Request) {
	connectionID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/check-connection/"))
	if connectionID == "" {
		http.Error(w, "missing connection id", http.StatusBadRequest)
		return
	}

	logging.Info(r.Context(), "check_connection.start", map[string]any{"connection_id": connectionID})
	status, err := h.CheckConnectionCore(r.Context(), connectionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	logging.Info(r.Context(), "check_connection.result", map[string]any{"connection_id": connectionID, "status": status})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func (h *Handler) GetToken(w http.ResponseWriter, r *http.Request) {
	connectionID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/token/"))
	if connectionID == "" {
		writeError(w, http.StatusBadRequest, "missing_fields", "missing connection id", nil)
		return
	}

	logging.Info(r.Context(), "get_token.start", map[string]any{"connection_id": connectionID})

	brokerURL := h.brokerBaseURL + "/connections/" + connectionID + "/token"
	httpReq, _ := http.NewRequest("GET", brokerURL, nil)
	if h.brokerAPIKey != "" {
		httpReq.Header.Set("X-API-Key", h.brokerAPIKey)
	}
	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		logging.Error(r.Context(), "get_token.broker_error", map[string]any{"error": err.Error()})
		writeError(w, http.StatusBadGateway, "broker_unavailable", "broker request failed", nil)
		return
	}
	defer resp.Body.Close()

	// Proxy response without logging body; do not store tokens
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	logging.Info(r.Context(), "get_token.proxy", map[string]any{"connection_id": connectionID, "status": resp.StatusCode})
	w.WriteHeader(resp.StatusCode)
	_, _ = ioCopy(w, resp.Body)
}

// GetTokenCore fetches the decrypted token JSON from the broker and returns it as a generic map.
func (h *Handler) GetTokenCore(ctx context.Context, connectionID string) (map[string]any, int, error) {
	brokerURL := h.brokerBaseURL + "/connections/" + connectionID + "/token"
	httpReq, _ := http.NewRequestWithContext(ctx, "GET", brokerURL, nil)
	if h.brokerAPIKey != "" {
		httpReq.Header.Set("X-API-Key", h.brokerAPIKey)
	}
	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("broker request failed: %w", err)
	}
	defer resp.Body.Close()

	var token map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("invalid token response: %w", err)
	}
	return token, resp.StatusCode, nil
}

// GetProvidersCore fetches provider metadata from the broker
func (h *Handler) GetProvidersCore(ctx context.Context) (map[string]any, error) {
	brokerURL := h.brokerBaseURL + "/providers/metadata"
	httpReq, _ := http.NewRequestWithContext(ctx, "GET", brokerURL, nil)
	if h.brokerAPIKey != "" {
		httpReq.Header.Set("X-API-Key", h.brokerAPIKey)
	}
	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBrokerUnavailable, err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, &BrokerStatusError{Status: resp.StatusCode}
	}

	var metadata map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBrokerInvalidResponse, err)
	}
	return metadata, nil
}

func (h *Handler) GetProviders(w http.ResponseWriter, r *http.Request) {
	logging.Info(r.Context(), "get_providers.start", nil)
	metadata, err := h.GetProvidersCore(r.Context())
	if err != nil {
		var be *BrokerStatusError
		if errors.As(err, &be) {
			writeError(w, http.StatusBadGateway, "broker_error", fmt.Sprintf("broker returned status %d", be.Status), map[string]any{"status": be.Status})
			return
		}
		writeError(w, http.StatusBadGateway, "broker_unavailable", "failed to fetch providers", map[string]any{"error": err.Error()})
		return
	}
	
	writeJSON(w, http.StatusOK, metadata)
}
