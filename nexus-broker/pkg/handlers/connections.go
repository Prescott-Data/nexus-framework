package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/audit"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ConnectionsHandler handles connection-related API requests
type ConnectionsHandler struct {
	svc   service.ConnectionService
	audit audit.Logger
}

// ConnectionsHandlerOption configures an optional dependency.
type ConnectionsHandlerOption func(*ConnectionsHandler)

// WithConnectionsAudit records revocations in the audit log. Revocation is a
// security event, so a deployment without an audit logger still works but
// loses the record of who destroyed which credential.
func WithConnectionsAudit(auditSvc audit.Logger) ConnectionsHandlerOption {
	return func(h *ConnectionsHandler) {
		h.audit = auditSvc
	}
}

// NewConnectionsHandler creates a new connections handler
func NewConnectionsHandler(svc service.ConnectionService, opts ...ConnectionsHandlerOption) *ConnectionsHandler {
	h := &ConnectionsHandler{svc: svc}
	for _, opt := range opts {
		opt(h)
	}
	return h
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

// revokeRequestBody is the optional body of a revoke request. Both fields are
// optional: a bare DELETE with no body is a valid revocation.
type revokeRequestBody struct {
	// Reason is recorded on the connection and in the audit log.
	Reason string `json:"reason,omitempty"`
	// WorkspaceID, when supplied, must match the connection's workspace. It
	// lets a workspace-scoped caller prove it owns the connection it is about
	// to destroy instead of relying on an unguessable UUID.
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// maxRevokeBody caps the request body. The payload is two short strings; any
// more is a client bug or an attempt to make the broker buffer.
const maxRevokeBody = 4 << 10

// Revoke handles DELETE /connections/{connectionID}
//
// Revocation destroys the stored credential and puts the connection in the
// terminal "revoked" state. It is idempotent: revoking an already-revoked
// connection returns 200 rather than an error, so a client that retries after
// a network timeout is not left unable to confirm the outcome.
func (h *ConnectionsHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "connectionID")
	connectionID, err := uuid.Parse(strings.TrimSpace(rawID))
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_connection_id", "connection_id must be a UUID")
		return
	}

	var body revokeRequestBody
	if r.Body != nil {
		raw, readErr := io.ReadAll(io.LimitReader(r.Body, maxRevokeBody))
		if readErr == nil && len(strings.TrimSpace(string(raw))) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				httputil.WriteError(w, http.StatusBadRequest, "invalid_json", "Request body must be a JSON object")
				return
			}
		}
	}

	// A workspace supplied in the query string is accepted too, so the endpoint
	// can be called with a plain DELETE and no body.
	workspaceID := strings.TrimSpace(body.WorkspaceID)
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = strings.TrimSpace(r.URL.Query().Get("reason"))
	}

	result, err := h.svc.RevokeConnection(r.Context(), service.RevokeRequest{
		ConnectionID: connectionID,
		WorkspaceID:  workspaceID,
		Reason:       reason,
	})
	if err != nil {
		h.logRevocation("connection.revoke_failed", &connectionID, map[string]interface{}{
			"error":  err.Error(),
			"reason": reason,
		}, r)
		writeServiceError(w, err)
		return
	}

	h.logRevocation("connection.revoked", &connectionID, map[string]interface{}{
		"provider_name":    result.ProviderName,
		"reason":           reason,
		"already_revoked":  result.AlreadyRevoked,
		"provider_revoked": result.ProviderRevoked,
		"sessions_closed":  result.SessionsClosed,
	}, r)

	httputil.WriteJSON(w, http.StatusOK, result)
}

func (h *ConnectionsHandler) logRevocation(eventType string, connectionID *uuid.UUID, data map[string]interface{}, r *http.Request) {
	if h.audit == nil {
		return
	}
	if err := h.audit.Log(eventType, connectionID, data, r); err != nil {
		log.Printf("[WARN] failed to write audit event %s: %v", eventType, err)
	}
}
