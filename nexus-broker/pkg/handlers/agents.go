package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
	"github.com/go-chi/chi/v5"
)

// AgentsHandler handles agent registry and session APIs.
type AgentsHandler struct {
	svc service.AgentService
}

func NewAgentsHandler(svc service.AgentService) *AgentsHandler {
	return &AgentsHandler{svc: svc}
}

// Register handles POST /admin/v1/agents.
func (h *AgentsHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req service.RegisterAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	agent, err := h.svc.RegisterAgent(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, agent)
}

// List handles GET /admin/v1/agents.
func (h *AgentsHandler) List(w http.ResponseWriter, r *http.Request) {
	agents, err := h.svc.ListAgents(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"agents": agents})
}

// CreateSession handles POST /v1/agent-sessions.
func (h *AgentsHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req service.AgentSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	resp, err := h.svc.RequestAgentSession(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, resp)
}

// GetSession handles GET /v1/agent-sessions/{sessionID}.
func (h *AgentsHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetAgentSession(r.Context(), chi.URLParam(r, "sessionID"))
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// CloseSession handles DELETE /v1/agent-sessions/{sessionID}.
func (h *AgentsHandler) CloseSession(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.CloseAgentSession(r.Context(), chi.URLParam(r, "sessionID"))
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "closed",
		"session_id": resp.SessionID,
		"active":     resp.Active,
	})
}
