package audit

import (
	"net/http"

	"github.com/google/uuid"
)

// Logger defines the interface for audit logging.
// Handlers depend on this interface so that a mock can be injected in tests.
type Logger interface {
	Log(eventType string, connectionID *uuid.UUID, data map[string]interface{}, r *http.Request) error
}
