package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"dromos-oauth-gateway/internal/broker"
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
	brokerClient  *broker.ClientWithResponses
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

	baseURL := strings.TrimRight(brokerBaseURL, "/")
	apiKey := strings.TrimSpace(getEnv("BROKER_API_KEY", ""))

	// Create the generated client
	client, err := broker.NewClientWithResponses(baseURL,
		broker.WithHTTPClient(httpClient),
		broker.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			if apiKey != "" {
				req.Header.Set("X-API-Key", apiKey)
			}
			return nil
		}),
	)
	if err != nil {
		// Should only happen if URL is invalid, but NewClient doesn't return error often.
		// We panic here because a bad base URL is a startup config error.
		panic(fmt.Errorf("failed to create broker client: %w", err))
	}

	return &Handler{
		brokerBaseURL: baseURL,
		stateKey:      stateKey,
		brokerClient:  client,
		providerCache: make(map[string]providerCacheEntry),
		brokerAPIKey:  apiKey,
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

	// Call Broker using generated client
	reqBody := broker.ConsentSpecRequest{
		WorkspaceId: in.UserID,
		ProviderId:  &providerID,
		Scopes:      &in.Scopes,
		ReturnUrl:   in.ReturnURL,
	}

	resp, err := h.brokerClient.PostAuthConsentSpecWithResponse(ctx, reqBody)
	if err != nil {
		logging.Error(ctx, "request_connection.core_broker_error", map[string]any{"error": err.Error()})
		return RequestConnectionOutput{}, fmt.Errorf("%w: %v", ErrBrokerUnavailable, err)
	}

	if resp.StatusCode() != http.StatusOK {
		logging.Error(ctx, "request_connection.core_broker_status", map[string]any{"status": resp.StatusCode()})
		return RequestConnectionOutput{}, &BrokerStatusError{Status: resp.StatusCode()}
	}

	if resp.JSON200 == nil {
		logging.Error(ctx, "request_connection.core_empty_response", nil)
		return RequestConnectionOutput{}, fmt.Errorf("%w: empty response", ErrBrokerInvalidResponse)
	}
	spec := resp.JSON200

	// The generated struct fields might be pointers if nullable in YAML.
	// In our YAML, they are strings (not nullable). oapi-codegen usually generates pointers for optional fields.
	// Checking yaml: fields are not 'required' in the response schema?
	// Wait, in openapi.yaml ConsentSpecResponse fields were NOT marked required explicitly in the schema object,
	// so they will likely be pointers.
	// Let's handle pointers safely.

	state := ""
	if spec.State != nil {
		state = *spec.State
	}

	authURL := ""
	if spec.AuthUrl != nil {
		authURL = *spec.AuthUrl
	}

	connectionID, err := VerifyAndExtractConnectionID(h.stateKey, state)
	if err != nil {
		logging.Error(ctx, "request_connection.core_state_invalid", map[string]any{"error": err.Error()})
		return RequestConnectionOutput{}, fmt.Errorf("%w: %v", ErrInvalidState, err)
	}

	var scopes []string
	if spec.Scopes != nil {
		scopes = *spec.Scopes
	}

	var pid string
	if spec.ProviderId != nil {
		pid = *spec.ProviderId
	}

	out := RequestConnectionOutput{
		AuthURL:      authURL,
		State:        state,
		Scopes:       scopes,
		ProviderID:   pid,
		ConnectionID: connectionID,
	}
	logging.Info(ctx, "request_connection.core_success", map[string]any{
		"provider_id":   pid,
		"connection_id": connectionID,
		"auth_url":      logging.RedactQuery(authURL),
	})
	return out, nil
}

// resolveProviderID looks up the provider_id by a human-friendly provider name via the broker.
func (h *Handler) resolveProviderID(ctx context.Context, providerName string) (string, error) {
	name := strings.TrimSpace(providerName)
	if name == "" {
		return "", fmt.Errorf("empty provider_name")
	}

	// Try canonical by-name endpoint
	resp, err := h.brokerClient.GetProvidersByNameNameWithResponse(ctx, name)
	if err == nil && resp.StatusCode() == http.StatusOK && resp.JSON200 != nil && resp.JSON200.Id != nil {
		return *resp.JSON200.Id, nil
	}

	// Fallback: list and filter
	listResp, err := h.brokerClient.GetProvidersWithResponse(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBrokerUnavailable, err)
	}
	if listResp.StatusCode() != http.StatusOK {
		return "", &BrokerStatusError{Status: listResp.StatusCode()}
	}
	if listResp.JSON200 == nil {
		return "", fmt.Errorf("%w: empty list", ErrBrokerInvalidResponse)
	}

	lower := strings.ToLower(name)
	var matchedID string
	matches := 0

	for _, p := range *listResp.JSON200 {
		if p.Name != nil && strings.ToLower(strings.TrimSpace(*p.Name)) == lower {
			if p.Id != nil {
				matchedID = *p.Id
				matches++
			}
		}
	}

	if matches == 0 {
		return "", fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	if matches > 1 {
		return "", fmt.Errorf("%w: %s", ErrProviderAmbiguous, name)
	}
	return matchedID, nil
}

// CheckConnectionCore probes broker token endpoint to infer status.
func (h *Handler) CheckConnectionCore(ctx context.Context, connectionID string) (string, error) {
	// We use the GetToken endpoint to check existence
	resp, err := h.brokerClient.GetConnectionsConnectionIDTokenWithResponse(ctx, connectionID)
	if err != nil {
		return "", fmt.Errorf("broker request failed: %w", err)
	}

	status := "pending"
	if resp.StatusCode() == http.StatusOK {
		status = "active"
	} else if resp.StatusCode() >= 400 && resp.StatusCode() < 500 {
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

	// Using generated client
	resp, err := h.brokerClient.GetConnectionsConnectionIDTokenWithResponse(r.Context(), connectionID)
	if err != nil {
		logging.Error(r.Context(), "get_token.broker_error", map[string]any{"error": err.Error()})
		writeError(w, http.StatusBadGateway, "broker_unavailable", "broker request failed", nil)
		return
	}

	logging.Info(r.Context(), "get_token.proxy", map[string]any{"connection_id": connectionID, "status": resp.StatusCode()})

	if resp.StatusCode() == http.StatusOK && resp.JSON200 != nil {
		writeJSON(w, http.StatusOK, resp.JSON200)
		return
	}

	// If not 200 OK or error body, just forward the status and generic error
	if resp.StatusCode() >= 400 {
		w.WriteHeader(resp.StatusCode())
		return
	}

	w.WriteHeader(resp.StatusCode())
}

// GetTokenCore fetches the decrypted token JSON from the broker and returns it as a generic map.
// Refactored to use generated client and convert struct back to map for backwards compat or generic usage.
func (h *Handler) GetTokenCore(ctx context.Context, connectionID string) (map[string]any, int, error) {
	resp, err := h.brokerClient.GetConnectionsConnectionIDTokenWithResponse(ctx, connectionID)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("broker request failed: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, resp.StatusCode(), nil
	}

	if resp.JSON200 == nil {
		return nil, resp.StatusCode(), fmt.Errorf("empty response")
	}

	// Convert TokenResponse struct back to map[string]any
	data, _ := json.Marshal(resp.JSON200)
	var tokenMap map[string]any
	_ = json.Unmarshal(data, &tokenMap)

	return tokenMap, http.StatusOK, nil
}

// RefreshConnectionCore forces a token refresh via the broker.
func (h *Handler) RefreshConnectionCore(ctx context.Context, connectionID string) (map[string]any, int, error) {
	resp, err := h.brokerClient.PostConnectionsConnectionIDRefreshWithResponse(ctx, connectionID)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("broker request failed: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, resp.StatusCode(), nil
	}

	if resp.JSON200 == nil {
		return nil, resp.StatusCode(), fmt.Errorf("empty response")
	}

	// Convert TokenResponse struct back to map[string]any
	data, _ := json.Marshal(resp.JSON200)
	var tokenMap map[string]any
	_ = json.Unmarshal(data, &tokenMap)

	return tokenMap, http.StatusOK, nil
}

func (h *Handler) RefreshConnection(w http.ResponseWriter, r *http.Request) {
	connectionID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/refresh/"))
	if connectionID == "" {
		writeError(w, http.StatusBadRequest, "missing_fields", "missing connection id", nil)
		return
	}

	logging.Info(r.Context(), "refresh_connection.start", map[string]any{"connection_id": connectionID})

	tokenMap, status, err := h.RefreshConnectionCore(r.Context(), connectionID)
	if err != nil {
		logging.Error(r.Context(), "refresh_connection.broker_error", map[string]any{"error": err.Error()})
		writeError(w, status, "broker_unavailable", "broker request failed", nil)
		return
	}

	if status != http.StatusOK {
		logging.Error(r.Context(), "refresh_connection.broker_status", map[string]any{"status": status})
		w.WriteHeader(status)
		return
	}

	logging.Info(r.Context(), "refresh_connection.success", map[string]any{"connection_id": connectionID})
	writeJSON(w, http.StatusOK, tokenMap)
}

// GetProvidersCore fetches provider metadata from the broker
func (h *Handler) GetProvidersCore(ctx context.Context) (map[string]any, error) {
	resp, err := h.brokerClient.GetProvidersMetadataWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBrokerUnavailable, err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, &BrokerStatusError{Status: resp.StatusCode()}
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("%w: empty response", ErrBrokerInvalidResponse)
	}

	// The generated type MetadataResponse is already a map[string]... structure
	// defined as AdditionalProperties in YAML.
	// oapi-codegen generates: type MetadataResponse map[string]map[string]interface{}
	// Wait, the YAML says:
	// MetadataResponse:
	//   additionalProperties:
	//     type: object
	//     additionalProperties: ...
	// So `resp.JSON200` should be `*MetadataResponse`.
	// We can cast it or marshal/unmarshal if types don't align exactly with map[string]any.
	// Since `MetadataResponse` IS a map type in Go (usually), let's see.
	// Ideally we return the struct, but the signature here asks for map[string]any.

	// Let's marshal/unmarshal to be safe and generic
	data, _ := json.Marshal(resp.JSON200)
	var metadata map[string]any
	_ = json.Unmarshal(data, &metadata)

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

// CreateProvider registers a new provider via the broker
func (h *Handler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	logging.Info(r.Context(), "create_provider.start", nil)

	var body broker.PostProvidersJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json body", nil)
		return
	}

	resp, err := h.brokerClient.PostProvidersWithResponse(r.Context(), body)
	if err != nil {
		logging.Error(r.Context(), "create_provider.broker_error", map[string]any{"error": err.Error()})
		writeError(w, http.StatusBadGateway, "broker_unavailable", "broker request failed", nil)
		return
	}

	if resp.StatusCode() != http.StatusCreated && resp.StatusCode() != http.StatusOK {
		logging.Error(r.Context(), "create_provider.broker_status", map[string]any{"status": resp.StatusCode()})
		// Try to read error body if available or just return status
		w.WriteHeader(resp.StatusCode())
		return
	}

	if resp.JSON201 != nil {
		writeJSON(w, http.StatusCreated, resp.JSON201)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
}

// GetProvider retrieves a single provider profile by ID
func (h *Handler) GetProvider(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	providerID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid provider id", nil)
		return
	}

	logging.Info(r.Context(), "get_provider.start", map[string]any{"id": idStr})

	resp, err := h.brokerClient.GetProvidersIdWithResponse(r.Context(), providerID)
	if err != nil {
		logging.Error(r.Context(), "get_provider.broker_error", map[string]any{"error": err.Error()})
		writeError(w, http.StatusBadGateway, "broker_unavailable", "broker request failed", nil)
		return
	}

	if resp.StatusCode() != http.StatusOK {
		w.WriteHeader(resp.StatusCode())
		return
	}

	if resp.JSON200 != nil {
		writeJSON(w, http.StatusOK, resp.JSON200)
	} else {
		w.WriteHeader(http.StatusOK)
	}
}

// UpdateProvider updates an existing provider by ID
func (h *Handler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	providerID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid provider id", nil)
		return
	}

	logging.Info(r.Context(), "update_provider.start", map[string]any{"id": idStr})

	var body broker.PutProvidersIdJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json body", nil)
		return
	}

	resp, err := h.brokerClient.PutProvidersIdWithResponse(r.Context(), providerID, body)
	if err != nil {
		logging.Error(r.Context(), "update_provider.broker_error", map[string]any{"error": err.Error()})
		writeError(w, http.StatusBadGateway, "broker_unavailable", "broker request failed", nil)
		return
	}

	if resp.StatusCode() != http.StatusOK {
		w.WriteHeader(resp.StatusCode())
		return
	}

	w.WriteHeader(http.StatusOK)
}

// PatchProvider updates specific fields of a provider by ID
func (h *Handler) PatchProvider(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	providerID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid provider id", nil)
		return
	}

	logging.Info(r.Context(), "patch_provider.start", map[string]any{"id": idStr})

	var body broker.PatchProvidersIdJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json body", nil)
		return
	}

	resp, err := h.brokerClient.PatchProvidersIdWithResponse(r.Context(), providerID, body)
	if err != nil {
		logging.Error(r.Context(), "patch_provider.broker_error", map[string]any{"error": err.Error()})
		writeError(w, http.StatusBadGateway, "broker_unavailable", "broker request failed", nil)
		return
	}

	if resp.StatusCode() != http.StatusOK {
		logging.Error(r.Context(), "patch_provider.broker_status", map[string]any{
			"status": resp.StatusCode(),
			"body":   string(resp.Body),
		})
		w.WriteHeader(resp.StatusCode())
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteProvider deletes a provider by ID
func (h *Handler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	providerID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid provider id", nil)
		return
	}

	logging.Info(r.Context(), "delete_provider.start", map[string]any{"id": idStr})

	resp, err := h.brokerClient.DeleteProvidersIdWithResponse(r.Context(), providerID)
	if err != nil {
		logging.Error(r.Context(), "delete_provider.broker_error", map[string]any{"error": err.Error()})
		writeError(w, http.StatusBadGateway, "broker_unavailable", "broker request failed", nil)
		return
	}

	if resp.StatusCode() != http.StatusOK {
		w.WriteHeader(resp.StatusCode())
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ProxyCallback forwards the OAuth callback to the Broker
func (h *Handler) ProxyCallback(w http.ResponseWriter, r *http.Request) {
	// We construct a target URL to the Broker's callback endpoint
	target, err := url.Parse(h.brokerBaseURL)
	if err != nil {
		logging.Error(r.Context(), "proxy_callback.parse_error", map[string]any{"error": err.Error()})
		http.Error(w, "invalid broker url", http.StatusInternalServerError)
		return
	}

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Update the director to set the correct path
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = "/auth/callback" // Force path to broker's callback
		req.Host = target.Host          // Set host header to broker's host

		// Pass query params (code, state) as is
		// originalDirector already copies URL, so query params are preserved
	}

	// Logging
	logging.Info(r.Context(), "proxy_callback.start", map[string]any{
		"path":  r.URL.Path,
		"query": r.URL.RawQuery,
	})

	proxy.ServeHTTP(w, r)
}
