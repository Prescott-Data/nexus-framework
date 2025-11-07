package provider

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// Store provides provider profile management
type Store struct {
	db *sqlx.DB
}

// NewStore creates a new provider store
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// Profile represents a provider profile
type Profile struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	AuthType        string     `json:"auth_type,omitempty"`
	AuthHeader      string     `json:"auth_header,omitempty"`
	ClientID        string     `json:"client_id,omitempty"`
	ClientSecret    string     `json:"client_secret,omitempty"`
	AuthURL         string     `json:"auth_url,omitempty"`
	TokenURL        string     `json:"token_url,omitempty"`
	Issuer          *string    `json:"issuer,omitempty"`
	EnableDiscovery bool       `json:"enable_discovery"`
	Scopes          []string   `json:"scopes"`
	DeletedAt       *time.Time `json:"-"`
}

// RegisterProfile registers a new provider profile from JSON
func (s *Store) RegisterProfile(profileJSON string) (*Profile, error) {
	var p Profile
	if err := json.Unmarshal([]byte(profileJSON), &p); err != nil {
		return nil, fmt.Errorf("invalid profile JSON: %w", err)
	}

	// Validate fields based on the authentication type
	switch p.AuthType {
	case "oauth2", "": // Default to "oauth2"
		if p.Name == "" || p.ClientID == "" || p.ClientSecret == "" || (!p.EnableDiscovery && (p.AuthURL == "" || p.TokenURL == "")) {
			return nil, fmt.Errorf("missing required oauth2 fields (name, client_id, client_secret, auth_url, token_url)")
		}
	case "api_key", "basic_auth":
		if p.Name == "" {
			return nil, fmt.Errorf("missing required field: name")
		}
	default:
		return nil, fmt.Errorf("unsupported auth_type: %s", p.AuthType)
	}

	query := `
		INSERT INTO provider_profiles (name, client_id, client_secret, auth_url, token_url, issuer, enable_discovery, scopes, auth_type, auth_header)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`

	var id uuid.UUID
	err := s.db.QueryRow(query, p.Name, p.ClientID, p.ClientSecret, p.AuthURL, p.TokenURL, p.Issuer, p.EnableDiscovery, p.Scopes, p.AuthType, p.AuthHeader).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider profile: %w", err)
	}

	p.ID = id
	return &p, nil
}

// GetProfile retrieves a provider profile by ID
func (s *Store) GetProfile(id uuid.UUID) (*Profile, error) {
	var p Profile
	query := `SELECT id, name, client_id, client_secret, auth_url, token_url, issuer, enable_discovery, scopes, auth_type, auth_header FROM provider_profiles WHERE id = $1 AND deleted_at IS NULL`

	row := s.db.QueryRow(query, id)
	err := row.Scan(&p.ID, &p.Name, &p.ClientID, &p.ClientSecret, &p.AuthURL, &p.TokenURL, &p.Issuer, &p.EnableDiscovery, &p.Scopes, &p.AuthType, &p.AuthHeader)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider profile: %w", err)
	}

	return &p, nil
}

// UpdateProfile updates an existing provider profile
func (s *Store) UpdateProfile(p *Profile) error {
	query := `
		UPDATE provider_profiles
		SET
			name = :name,
			client_id = :client_id,
			client_secret = :client_secret,
			auth_url = :auth_url,
			token_url = :token_url,
			issuer = :issuer,
			enable_discovery = :enable_discovery,
			scopes = :scopes,
			auth_type = :auth_type,
			auth_header = :auth_header,
			updated_at = NOW()
		WHERE id = :id AND deleted_at IS NULL`

	_, err := s.db.NamedExec(query, p)
	if err != nil {
		return fmt.Errorf("failed to update provider profile: %w", err)
	}

	return nil
}

// DeleteProfile soft-deletes a provider profile by ID
func (s *Store) DeleteProfile(id uuid.UUID) error {
	query := `UPDATE provider_profiles SET deleted_at = NOW() WHERE id = $1`
	_, err := s.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete provider profile: %w", err)
	}
	return nil
}
