package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
)

type SAMLHandler struct {
	svc service.ConnectionService
}

func NewSAMLHandler(svc service.ConnectionService) *SAMLHandler {
	return &SAMLHandler{svc: svc}
}

func (h *SAMLHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	providerID, err := uuid.Parse(chi.URLParam(r, "providerID"))
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_provider_id", "Invalid provider ID")
		return
	}

	metadata, err := h.svc.GetSAMLMetadata(r.Context(), providerID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	_, _ = w.Write(metadata)
}

func (h *SAMLHandler) ACS(w http.ResponseWriter, r *http.Request) {
	returnURL, err := h.svc.ExchangeSAMLResponse(r.Context(), r)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	http.Redirect(w, r, returnURL, http.StatusFound)
}
