package domain

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Connection represents a user's connection to a provider
type Connection struct {
	ID           uuid.UUID
	WorkspaceID  string
	ProviderID   uuid.UUID
	CodeVerifier sql.NullString
	Scopes       []string
	ReturnURL    string
	Status       string
	ExpiresAt    time.Time
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
