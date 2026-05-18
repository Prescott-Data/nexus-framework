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

// Token represents an encrypted token at rest
type Token struct {
	ConnectionID  uuid.UUID
	EncryptedData string
	ExpiresAt     *time.Time
}
