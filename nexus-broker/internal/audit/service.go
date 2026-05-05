package audit

import (
	"encoding/json"
	"fmt"
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
		// Extract IP — validate with net.ParseIP to avoid storing arbitrary text in the inet column.
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ip := strings.TrimSpace(strings.Split(fwd, ",")[0])
			if net.ParseIP(ip) != nil {
				ipVal = &ip
			}
		} else {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err == nil && net.ParseIP(host) != nil {
				ipVal = &host
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
		var err error
		eventDataJSON, err = json.Marshal(data)
		if err != nil {
			return fmt.Errorf("audit: failed to marshal event data: %w", err)
		}
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
