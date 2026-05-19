package handlers

import (
	"net/http"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
)

// ConnectionsHandler handles connection-related API requests
type ConnectionsHandler struct {
	svc service.ConnectionService
}

// NewConnectionsHandler creates a new connections handler
func NewConnectionsHandler(svc service.ConnectionService) *ConnectionsHandler {
	return &ConnectionsHandler{svc: svc}
}

// List handles GET /connections?workspace_id=ws-123
// Returns all non-pending connections for a workspace with health status.
func (h *ConnectionsHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspace_id")

	if workspaceID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "missing_workspace_id", "workspace_id query parameter is required")
		return
	}

	connections, err := h.svc.ListConnections(r.Context(), workspaceID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, connections)
}
