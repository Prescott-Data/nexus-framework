package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
)

// ConsentHandler handles OAuth consent flow
type ConsentHandler struct {
	svc            service.ConnectionService
	consentsMetric prometheus.Counter
	consentsOpenID prometheus.Counter
}

// ConsentHandlerConfig holds the dependencies for ConsentHandler
type ConsentHandlerConfig struct {
	Service service.ConnectionService
}

// NewConsentHandler creates a new consent handler
func NewConsentHandler(cfg ConsentHandlerConfig) *ConsentHandler {
	metric := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "oauth_consents_created_total",
		Help: "Total OAuth consents created",
	})
	metricOpenID := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "oauth_consents_with_openid_total",
		Help: "Total OAuth consents where openid scope was requested",
	})

	collectors := []prometheus.Collector{metric, metricOpenID}
	for _, c := range collectors {
		if err := prometheus.Register(c); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				panic(err)
			}
		}
	}

	return &ConsentHandler{
		svc:            cfg.Service,
		consentsMetric: metric,
		consentsOpenID: metricOpenID,
	}
}

// GetSpec handles POST /auth/consent-spec
func (h *ConsentHandler) GetSpec(w http.ResponseWriter, r *http.Request) {
	var request service.CreateConsentRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	response, err := h.svc.CreateConsentSpec(r.Context(), request)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "consent_spec_failed", err.Error())
		return
	}

	h.consentsMetric.Inc()
	for _, s := range request.Scopes {
		if s == "openid" {
			h.consentsOpenID.Inc()
			break
		}
	}

	httputil.WriteJSON(w, http.StatusOK, response)
}
