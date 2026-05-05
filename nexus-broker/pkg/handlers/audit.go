package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/storage"
	"github.com/jmoiron/sqlx"
)

// AuditHandler handles audit log queries
type AuditHandler struct {
	db *sqlx.DB
}

// NewAuditHandler creates a new audit handler
func NewAuditHandler(db *sqlx.DB) *AuditHandler {
	return &AuditHandler{db: db}
}

// List handles GET /audit to retrieve recent audit events
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	eventType := r.URL.Query().Get("event_type")
	sinceStr := r.URL.Query().Get("since")
	limitStr := r.URL.Query().Get("limit")

	limit := 50
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 && parsedLimit <= 1000 {
			limit = parsedLimit
		}
	}

	query := `SELECT id, connection_id, event_type, event_data, ip_address, user_agent, created_at 
			  FROM audit_events WHERE 1=1`
	args := []interface{}{}
	argIndex := 1

	if eventType != "" {
		query += ` AND event_type = $` + strconv.Itoa(argIndex)
		args = append(args, eventType)
		argIndex++
	}

	if sinceStr != "" {
		since, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "invalid_since", "since parameter must be a valid RFC3339 timestamp")
			return
		}
		query += ` AND created_at >= $` + strconv.Itoa(argIndex)
		args = append(args, since)
		argIndex++
	}

	query += ` ORDER BY created_at DESC LIMIT $` + strconv.Itoa(argIndex)
	args = append(args, limit)

	var events []storage.AuditEvent
	if err := h.db.Select(&events, query, args...); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "query_failed", "Failed to query audit events")
		return
	}

	// Make sure we return an empty array instead of null for no results
	if events == nil {
		events = []storage.AuditEvent{}
	}

	httputil.WriteJSON(w, http.StatusOK, events)
}
