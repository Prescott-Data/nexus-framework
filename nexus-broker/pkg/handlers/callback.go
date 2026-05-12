package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/audit"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
)

// CallbackHandler handles OAuth callback and token exchange
type CallbackHandler struct {
	svc                   service.ConnectionService
	audit                 *audit.Service
	metricExchangeSuccess prometheus.Counter
	metricExchangeError   prometheus.Counter
	histogramExchangeDur  prometheus.Histogram
	metricIDTokens        prometheus.Counter
	metricTokenGet        *prometheus.CounterVec
	metricRefreshSuccess  prometheus.Counter
	metricRefreshError    prometheus.Counter
	histogramRefreshDur   prometheus.Histogram
	metricCaptureSuccess  prometheus.Counter
	metricCaptureError    prometheus.Counter
}

// CallbackHandlerConfig holds the dependencies for CallbackHandler
type CallbackHandlerConfig struct {
	Service service.ConnectionService
	Audit   *audit.Service
}

// NewCallbackHandler creates a new callback handler
func NewCallbackHandler(cfg CallbackHandlerConfig) *CallbackHandler {
	success := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "oauth_token_exchanges_total",
		Help:        "Total OAuth token exchanges",
		ConstLabels: prometheus.Labels{"status": "success"},
	})
	failure := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "oauth_token_exchanges_total",
		Help:        "Total OAuth token exchanges",
		ConstLabels: prometheus.Labels{"status": "error"},
	})
	hist := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "oauth_exchange_duration_seconds",
		Help:    "Duration of token exchange requests",
		Buckets: prometheus.DefBuckets,
	})
	idTokens := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "oauth_id_tokens_returned_total",
		Help: "Total number of times an id_token was returned by provider",
	})
	tokenGet := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "oauth_token_get_total",
		Help: "Token retrievals by provider and whether id_token present",
	}, []string{"provider", "has_id_token"})

	refreshSuccess := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "oauth_token_refreshes_total",
		Help:        "Total OAuth token refreshes",
		ConstLabels: prometheus.Labels{"status": "success"},
	})
	refreshFailure := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "oauth_token_refreshes_total",
		Help:        "Total OAuth token refreshes",
		ConstLabels: prometheus.Labels{"status": "error"},
	})
	refreshHist := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "oauth_refresh_duration_seconds",
		Help:    "Duration of token refresh requests",
		Buckets: prometheus.DefBuckets,
	})

	captureSuccess := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "oauth_credential_captures_total",
		Help:        "Total credential captures (API key / static token)",
		ConstLabels: prometheus.Labels{"status": "success"},
	})
	captureFailure := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "oauth_credential_captures_total",
		Help:        "Total credential captures (API key / static token)",
		ConstLabels: prometheus.Labels{"status": "error"},
	})

	collectors := []prometheus.Collector{
		success, failure, hist, idTokens, tokenGet,
		refreshSuccess, refreshFailure, refreshHist,
		captureSuccess, captureFailure,
	}
	for _, c := range collectors {
		if err := prometheus.Register(c); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				panic(err)
			}
		}
	}

	return &CallbackHandler{
		svc:                   cfg.Service,
		audit:                 cfg.Audit,
		metricExchangeSuccess: success,
		metricExchangeError:   failure,
		histogramExchangeDur:  hist,
		metricIDTokens:        idTokens,
		metricTokenGet:        tokenGet,
		metricRefreshSuccess:  refreshSuccess,
		metricRefreshError:    refreshFailure,
		histogramRefreshDur:   refreshHist,
		metricCaptureSuccess:  captureSuccess,
		metricCaptureError:    captureFailure,
	}
}

// writeServiceError unwraps a ServiceError from the service layer and writes
// the correct HTTP status code and machine-readable error code. Falls back
// to 500 for unexpected error types.
func writeServiceError(w http.ResponseWriter, err error) {
	var svcErr *service.ServiceError
	if errors.As(err, &svcErr) {
		if svcErr.HTTPStatus >= 500 {
			log.Printf("[ERROR] %d %s: %v (cause: %v)", svcErr.HTTPStatus, svcErr.Code, svcErr.Message, svcErr.Err)
		}
		httputil.WriteError(w, svcErr.HTTPStatus, svcErr.Code, svcErr.Message)
	} else {
		log.Printf("[ERROR] 500 internal_error: unexpected error: %v", err)
		httputil.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}

// Handle handles GET /auth/callback
func (h *CallbackHandler) Handle(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")
	errorDesc := r.URL.Query().Get("error_description")

	if errorParam != "" {
		h.logAuditEvent(nil, "oauth_error", map[string]string{
			"error":       errorParam,
			"description": errorDesc,
		}, r)
		httputil.WriteError(w, http.StatusBadRequest, "oauth_error", fmt.Sprintf("OAuth error: %s - %s", errorParam, errorDesc))
		return
	}

	if code == "" || state == "" {
		httputil.WriteError(w, http.StatusBadRequest, "missing_params", "Missing code or state parameter")
		return
	}

	start := time.Now()
	returnURL, hasIDToken, err := h.svc.ExchangeCodeForTokens(r.Context(), state, code, errorParam, errorDesc)
	h.histogramExchangeDur.Observe(time.Since(start).Seconds())

	if err != nil {
		h.logAuditEvent(nil, "token_exchange_failed", map[string]string{"error": err.Error()}, r)
		h.metricExchangeError.Inc()
		writeServiceError(w, err)
		return
	}

	if hasIDToken {
		h.metricIDTokens.Inc()
	}

	h.metricExchangeSuccess.Inc()
	h.logAuditEvent(nil, "oauth_flow_completed", map[string]string{}, r)

	http.Redirect(w, r, returnURL, http.StatusFound)
}

// GetCaptureSchema serves a JSON schema for the credential capture form.
func (h *CallbackHandler) GetCaptureSchema(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")

	providerName, schema, err := h.svc.GetCaptureSchema(r.Context(), state)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	type SchemaResponse struct {
		ProviderName string          `json:"provider_name"`
		Schema       json.RawMessage `json:"schema"`
	}

	response := SchemaResponse{
		ProviderName: providerName,
		Schema:       schema,
	}

	httputil.WriteJSON(w, http.StatusOK, response)
}

// SaveCredential handles the submission of the credential capture form.
func (h *CallbackHandler) SaveCredential(w http.ResponseWriter, r *http.Request) {
	var reqBody struct {
		State       string                 `json:"state"`
		Credentials map[string]interface{} `json:"credentials"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}

	returnURL, err := h.svc.SaveCredential(r.Context(), reqBody.State, reqBody.Credentials)
	if err != nil {
		h.metricCaptureError.Inc()
		writeServiceError(w, err)
		return
	}

	h.metricCaptureSuccess.Inc()
	http.Redirect(w, r, returnURL, http.StatusFound)
}

// ResolveToken handles GET /connections/resolve?workspace_id=X&provider_name=Y
func (h *CallbackHandler) ResolveToken(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspace_id")
	providerName := r.URL.Query().Get("provider_name")

	if workspaceID == "" || providerName == "" {
		httputil.WriteError(w, http.StatusBadRequest, "missing_params", "Missing workspace_id or provider_name parameter")
		return
	}

	response, provName, err := h.svc.GetTokenByWorkspaceAndProvider(r.Context(), workspaceID, providerName)
	if err != nil {
		var svcErr *service.ServiceError
		if errors.As(err, &svcErr) && svcErr.Code == "attention_required" {
			httputil.WriteJSON(w, http.StatusConflict, map[string]string{
				"error":  "attention_required",
				"detail": svcErr.Message,
			})
			return
		}
		// Try not to log error if it's just not found yet to avoid spam, but log it for now
		h.logAuditEvent(nil, "token_resolve_failed", map[string]string{"error": err.Error(), "workspace_id": workspaceID, "provider": providerName}, r)
		writeServiceError(w, err)
		return
	}

	hasID := "false"
	if creds, ok := response["credentials"].(map[string]interface{}); ok {
		if _, ok := creds["id_token"]; ok {
			hasID = "true"
		}
	}
	h.metricTokenGet.WithLabelValues(provName, hasID).Inc()

	h.logAuditEvent(nil, "token_resolved", map[string]string{"workspace_id": workspaceID, "provider": providerName}, r)
	httputil.WriteJSON(w, http.StatusOK, response)
}

// GetToken handles GET /connections/{connection_id}/token
func (h *CallbackHandler) GetToken(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 3 {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_path", "Invalid path")
		return
	}
	connectionIDStr := pathParts[len(pathParts)-2]

	connectionID, err := uuid.Parse(connectionIDStr)
	if err != nil {
		h.logAuditEvent(nil, "token_retrieval_failed", map[string]string{"error": "invalid connection ID", "id": connectionIDStr}, r)
		httputil.WriteError(w, http.StatusBadRequest, "invalid_connection_id", "Invalid connection ID")
		return
	}

	response, providerName, err := h.svc.GetToken(r.Context(), connectionID)
	if err != nil {
		var svcErr *service.ServiceError
		if errors.As(err, &svcErr) && svcErr.Code == "attention_required" {
			httputil.WriteJSON(w, http.StatusConflict, map[string]string{
				"error":  "attention_required",
				"detail": svcErr.Message,
			})
			return
		}
		h.logAuditEvent(&connectionID, "token_retrieval_failed", map[string]string{"error": err.Error()}, r)
		writeServiceError(w, err)
		return
	}

	hasID := "false"
	if creds, ok := response["credentials"].(map[string]interface{}); ok {
		if _, ok := creds["id_token"]; ok {
			hasID = "true"
		}
	}
	h.metricTokenGet.WithLabelValues(providerName, hasID).Inc()

	h.logAuditEvent(&connectionID, "token_retrieved", map[string]string{}, r)
	httputil.WriteJSON(w, http.StatusOK, response)
}

// Refresh handles POST /connections/{connection_id}/refresh
func (h *CallbackHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_path", "Invalid path")
		return
	}
	idStr := parts[len(parts)-2]
	connectionID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_connection_id", "Invalid connection ID")
		return
	}

	start := time.Now()
	res, err := h.svc.Refresh(r.Context(), connectionID)
	h.histogramRefreshDur.Observe(time.Since(start).Seconds())

	if err != nil {
		h.metricRefreshError.Inc()
		if res != nil && res.StatusCode >= 400 && res.StatusCode < 500 {
			h.logAuditEvent(&connectionID, "token_refresh_fatal", map[string]string{"error": err.Error(), "status_code": fmt.Sprintf("%d", res.StatusCode)}, r)
			httputil.WriteJSON(w, http.StatusConflict, map[string]string{
				"error":  "attention_required",
				"detail": "The connection credentials are invalid or expired and cannot be refreshed. User re-consent is required.",
			})
			return
		}
		httputil.WriteError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}

	h.metricRefreshSuccess.Inc()
	httputil.WriteJSON(w, http.StatusOK, res.Tokens)
}

// logAuditEvent logs an audit event
func (h *CallbackHandler) logAuditEvent(connectionID *uuid.UUID, eventType string, data map[string]string, r *http.Request) {
	if h.audit == nil {
		return
	}

	auditData := make(map[string]interface{})
	for k, v := range data {
		auditData[k] = v
	}

	if err := h.audit.Log(eventType, connectionID, auditData, r); err != nil {
		log.Printf("audit: failed to log %s (connection_id=%v): %v", eventType, connectionID, err)
	}
}
