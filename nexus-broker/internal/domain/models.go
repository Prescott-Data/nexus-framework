package domain

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Connection represents a user's connection to a provider
type Connection struct {
	ID                uuid.UUID
	WorkspaceID       string
	ProviderID        uuid.UUID
	CodeVerifier      sql.NullString
	Scopes            []string
	ReturnURL         string
	Status            string
	ExpiresAt         time.Time
	LastHealthCheckAt sql.NullTime
	HealthStatus      string
}

// ConnectionWithProvider joins connection and basic provider info
type ConnectionWithProvider struct {
	Connection
	ProviderName     string
	AuthType         string
	AuthHeader       string
	APIBaseURL       string
	UserInfoEndpoint string
	ProviderParams   *json.RawMessage
}

// ConnectionSummary is a lightweight view of a connection for frontend listing.
// It deliberately omits credentials and internal fields.
type ConnectionSummary struct {
	ID                uuid.UUID  `json:"id"`
	ProviderID        uuid.UUID  `json:"provider_id"`
	ProviderName      string     `json:"provider_name"`
	AuthType          string     `json:"auth_type"`
	Status            string     `json:"status"`
	Scopes            []string   `json:"scopes"`
	HealthStatus      string     `json:"health_status"`
	LastHealthCheckAt *time.Time `json:"last_health_check_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// Token represents an encrypted token at rest
type Token struct {
	ConnectionID  uuid.UUID
	EncryptedData string
	ExpiresAt     *time.Time
}

// Agent represents a registered agent principal and its authorization ceiling.
type Agent struct {
	ID            string    `json:"agent_id"`
	Description   string    `json:"description,omitempty"`
	AllowedScopes []string  `json:"allowed_scopes"`
	CreatedAt     time.Time `json:"created_at"`
	Active        bool      `json:"active"`
}

// AgentSession records a short-lived scoped credential grant for an agent.
type AgentSession struct {
	SessionID      string     `json:"session_id"`
	AgentID        string     `json:"agent_id"`
	ConnectionID   uuid.UUID  `json:"connection_id"`
	ScopesGranted  []string   `json:"scopes_granted"`
	ExpiresAt      time.Time  `json:"expires_at"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
	OBO            bool       `json:"obo"`
	ActingFor      string     `json:"acting_for,omitempty"`
	TenantID       string     `json:"tenant_id,omitempty"`
	ClearanceLevel int        `json:"clearance_level"`
	CreatedAt      time.Time  `json:"created_at"`
}

// Active reports whether the session can still be used.
func (s AgentSession) Active(now time.Time) bool {
	return s.ClosedAt == nil && s.ExpiresAt.After(now)
}
