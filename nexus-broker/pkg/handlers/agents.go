package handlers

import (
	"encoding/json"
	"errors"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/audit"
	"github.com/google/uuid"
	"log"
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
	audit          *audit.Service
}

// WithAgentsAudit records every session issued or refused, with its subject,
// agent and operation: a session carries an access token, so it is a release.
func WithAgentsAudit(svc *audit.Service) AgentsHandlerOption {
	return func(h *AgentsHandler) { h.audit = svc }
}

func (h *AgentsHandler) record(r *http.Request, event string, connectionID *uuid.UUID, data map[string]string) {
	if h.audit == nil {
		return
	}
	payload := map[string]interface{}{}
	for k, v := range releaseRecord(r, data) {
		payload[k] = v
	}
	if err := h.audit.Log(event, connectionID, payload, r); err != nil {
		log.Printf("audit: failed to log %s: %v", event, err)
	}
}

func (h *AgentsHandler) refuse(w http.ResponseWriter, r *http.Request, status int, code, message string, req service.AgentSessionRequest) {
	h.record(r, "agent_session_refused", nil, map[string]string{"reason": code, "agent_id": req.AgentID, "acting_for": req.ActingFor})
	httputil.WriteError(w, status, code, message)
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
		if req.ActingFor != "" && req.ActingFor != subject {
			h.refuse(w, r, http.StatusBadRequest, "subject_conflict", "acting_for and "+HeaderSubject+" name different subjects.", req)
			return
		}
		req.ActingFor = subject
	}
	if h.subjectBinding == "off" {
		req.ActingFor = ""
	} else if h.subjectBinding == "required" && strings.TrimSpace(req.ActingFor) == "" {
		h.refuse(w, r, http.StatusForbidden, "subject_required", "An agent session must name the subject it acts for.", req)
		return
	}

	resp, err := h.svc.RequestAgentSession(r.Context(), req)
	if err != nil {
		var svcErr *service.ServiceError
		if errors.As(err, &svcErr) && svcErr.Code == "subject_mismatch" {
			h.record(r, "agent_session_refused", nil, map[string]string{"reason": svcErr.Code, "agent_id": req.AgentID, "acting_for": req.ActingFor})
		}
		writeServiceError(w, err)
		return
	}
	connectionID := resp.ConnectionID
	h.record(r, "agent_session_issued", &connectionID, map[string]string{
		"agent_id": resp.AgentID, "session_id": resp.SessionID, "acting_for": resp.ActingFor,
	})

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
