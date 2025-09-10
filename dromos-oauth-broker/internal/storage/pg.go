package storage

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
)

type DB struct {
	*sqlx.DB
}

// NewDB creates a new database connection
func NewDB(dsn string) (*DB, error) {
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return &DB{db}, nil
}

// ProviderProfile represents an OAuth provider configuration
type ProviderProfile struct {
	ID           uuid.UUID `db:"id" json:"id"`
	Name         string    `db:"name" json:"name"`
	ClientID     string    `db:"client_id" json:"client_id"`
	ClientSecret string    `db:"client_secret" json:"client_secret"`
	AuthURL      string    `db:"auth_url" json:"auth_url"`
	TokenURL     string    `db:"token_url" json:"token_url"`
	Scopes       []string  `db:"scopes" json:"scopes"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

// Connection represents an OAuth connection flow
type Connection struct {
	ID           uuid.UUID `db:"id" json:"id"`
	WorkspaceID  string    `db:"workspace_id" json:"workspace_id"`
	ProviderID   uuid.UUID `db:"provider_id" json:"provider_id"`
	Status       string    `db:"status" json:"status"`
	CodeVerifier string    `db:"code_verifier" json:"code_verifier"`
	Scopes       []string  `db:"scopes" json:"scopes"`
	ReturnURL    string    `db:"return_url" json:"return_url"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	ExpiresAt    time.Time `db:"expires_at" json:"expires_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

// Token represents encrypted OAuth tokens
type Token struct {
	ID            uuid.UUID  `db:"id" json:"id"`
	ConnectionID  uuid.UUID  `db:"connection_id" json:"connection_id"`
	EncryptedData string     `db:"encrypted_data" json:"encrypted_data"`
	ExpiresAt     *time.Time `db:"expires_at" json:"expires_at,omitempty"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
}

// AuditEvent represents audit logging
type AuditEvent struct {
	ID           uuid.UUID  `db:"id" json:"id"`
	ConnectionID *uuid.UUID `db:"connection_id" json:"connection_id,omitempty"`
	EventType    string     `db:"event_type" json:"event_type"`
	EventData    string     `db:"event_data" json:"event_data,omitempty"`
	IPAddress    string     `db:"ip_address" json:"ip_address,omitempty"`
	UserAgent    string     `db:"user_agent" json:"user_agent,omitempty"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
}

// Provider operations
func (db *DB) CreateProviderProfile(p *ProviderProfile) error {
	query := `
		INSERT INTO provider_profiles (name, client_id, client_secret, auth_url, token_url, scopes)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at`

	return db.QueryRowx(query, p.Name, p.ClientID, p.ClientSecret, p.AuthURL, p.TokenURL, pq.Array(p.Scopes)).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

func (db *DB) GetProviderProfile(id uuid.UUID) (*ProviderProfile, error) {
	var p ProviderProfile
	query := `SELECT * FROM provider_profiles WHERE id = $1`
	err := db.Get(&p, query, id)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("provider profile not found")
	}
	return &p, err
}

// Connection operations
func (db *DB) CreateConnection(c *Connection) error {
	query := `
		INSERT INTO connections (workspace_id, provider_id, code_verifier, scopes, return_url, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at`

	return db.QueryRowx(query, c.WorkspaceID, c.ProviderID, c.CodeVerifier, pq.Array(c.Scopes), c.ReturnURL, c.ExpiresAt).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

func (db *DB) GetConnection(id uuid.UUID) (*Connection, error) {
	var c Connection
	query := `SELECT * FROM connections WHERE id = $1 AND status = 'pending' AND expires_at > NOW()`
	err := db.Get(&c, query, id)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("connection not found or expired")
	}
	return &c, err
}

func (db *DB) UpdateConnectionStatus(id uuid.UUID, status string) error {
	query := `UPDATE connections SET status = $1, updated_at = NOW() WHERE id = $2`
	_, err := db.Exec(query, status, id)
	return err
}

// Token operations
func (db *DB) CreateToken(t *Token) error {
	query := `
		INSERT INTO tokens (connection_id, encrypted_data, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`

	return db.QueryRowx(query, t.ConnectionID, t.EncryptedData, t.ExpiresAt).Scan(&t.ID, &t.CreatedAt)
}

// Audit operations
func (db *DB) CreateAuditEvent(e *AuditEvent) error {
	query := `
		INSERT INTO audit_events (connection_id, event_type, event_data, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`

	return db.QueryRowx(query, e.ConnectionID, e.EventType, e.EventData, e.IPAddress, e.UserAgent).Scan(&e.ID, &e.CreatedAt)
}
