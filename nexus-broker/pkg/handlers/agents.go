package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
	"github.com/go-chi/chi/v5"
)

// AgentsHandler handles agent registry and session APIs.
type AgentsHandler struct {
	svc            service.AgentService
	subjectBinding string
}

// AgentsHandlerOption configures an AgentsHandler.
type AgentsHandlerOption func(*AgentsHandler)

// WithAgentSubjectBinding sets how sessions are bound to the subject they act for
// ("off", "optional" or "required"; see CallbackHandlerConfig.SubjectBinding).
func WithAgentSubjectBinding(mode string) AgentsHandlerOption {
	return func(h *AgentsHandler) { h.subjectBinding = bindingMode(mode) }
}

func NewAgentsHandler(svc service.AgentService, opts ...AgentsHandlerOption) *AgentsHandler {
	h := &AgentsHandler{svc: svc, subjectBinding: "optional"}
	for _, opt := range opts {
		opt(h)
	}
	return h
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
	if subject := Subject(r); subject != "" && h.subjectBinding != "off" {
		if req.ActingFor != "" && !strings.EqualFold(req.ActingFor, subject) {
			httputil.WriteError(w, http.StatusBadRequest, "subject_conflict", "acting_for and "+HeaderSubject+" name different subjects.")
			return
		}
		req.ActingFor = subject
	}
	if h.subjectBinding == "off" {
		req.ActingFor = ""
	} else if h.subjectBinding == "required" && strings.TrimSpace(req.ActingFor) == "" {
		httputil.WriteError(w, http.StatusForbidden, "subject_required", "An agent session must name the subject it acts for.")
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
