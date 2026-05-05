package audit

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type Service struct {
	db *sqlx.DB
}

func NewService(db *sqlx.DB) *Service {
	return &Service{db: db}
}

func (s *Service) Log(eventType string, connectionID *uuid.UUID, data map[string]interface{}, r *http.Request) error {
	var ipVal *string
	var userAgent *string

	if r != nil {
		// Extract IP
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ip := strings.Split(fwd, ",")[0]
			if comma := strings.IndexByte(ip, ','); comma != -1 {
				ip = strings.TrimSpace(ip[:comma])
			}
			ipVal = &ip
		} else {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err == nil {
				ipVal = &host
			} else {
				ipVal = &r.RemoteAddr
			}
		}

		// Extract User-Agent
		ua := r.Header.Get("User-Agent")
		if ua != "" {
			userAgent = &ua
		}
	}

	var eventDataJSON []byte
	if data != nil {
		eventDataJSON, _ = json.Marshal(data)
	}

	query := `
		INSERT INTO audit_events (connection_id, event_type, event_data, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)`

	var eventDataArg interface{}
	if len(eventDataJSON) > 0 {
		eventDataArg = string(eventDataJSON)
	}

	_, err := s.db.Exec(query, connectionID, eventType, eventDataArg, ipVal, userAgent)
	return err
}
