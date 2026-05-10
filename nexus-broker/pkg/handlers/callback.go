package handlers

import (
	"encoding/json"
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

	collectors := []prometheus.Collector{success, failure, hist, idTokens, tokenGet}
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
	returnURL, err := h.svc.ExchangeCodeForTokens(r.Context(), state, code, errorParam, errorDesc)
	h.histogramExchangeDur.Observe(time.Since(start).Seconds())

	if err != nil {
		h.logAuditEvent(nil, "token_exchange_failed", map[string]string{"error": err.Error()}, r)
		h.metricExchangeError.Inc()
		httputil.WriteError(w, http.StatusInternalServerError, "token_exchange_failed", err.Error())
		return
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
		httputil.WriteError(w, http.StatusBadRequest, "schema_error", err.Error())
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
		httputil.WriteError(w, http.StatusInternalServerError, "credential_save_failed", err.Error())
		return
	}

	http.Redirect(w, r, returnURL, http.StatusFound)
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

	response, err := h.svc.GetToken(r.Context(), connectionID)
	if err != nil {
		if err.Error() == "attention_required" {
			httputil.WriteJSON(w, http.StatusConflict, map[string]string{
				"error":  "attention_required",
				"detail": "Connection requires attention. The user must re-authenticate.",
			})
			return
		}
		h.logAuditEvent(&connectionID, "token_retrieval_failed", map[string]string{"error": err.Error()}, r)
		httputil.WriteError(w, http.StatusInternalServerError, "token_error", err.Error())
		return
	}

	hasID := "false"
	if creds, ok := response["credentials"].(map[string]interface{}); ok {
		if _, ok := creds["id_token"]; ok {
			hasID = "true"
		}
	}
	h.metricTokenGet.WithLabelValues("unknown", hasID).Inc()

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

	res, err := h.svc.Refresh(r.Context(), connectionID)
	if err != nil {
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
