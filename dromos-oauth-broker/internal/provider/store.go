package provider

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
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
	ID              uuid.UUID        `json:"id"`
	Name            string           `json:"name"`
	AuthType        string           `json:"auth_type,omitempty"`
	AuthHeader      string           `json:"auth_header,omitempty"`
	ClientID        string           `json:"client_id,omitempty"`
	ClientSecret    string           `json:"client_secret,omitempty"`
	AuthURL         string           `json:"auth_url,omitempty"`
	TokenURL        string           `json:"token_url,omitempty"`
	Issuer          *string          `json:"issuer,omitempty"`
	EnableDiscovery bool             `json:"enable_discovery"`
	Scopes          []string         `json:"scopes"`
	Params          *json.RawMessage `json:"params,omitempty"`
	DeletedAt       *time.Time       `json:"-"`
}

// RegisterProfile registers a new provider profile from JSON
func (s *Store) RegisterProfile(profileJSON string) (*Profile, error) {
	var p Profile
	if err := json.Unmarshal([]byte(profileJSON), &p); err != nil {
		return nil, fmt.Errorf("invalid profile JSON: %w", err)
	}

	// Validate provider name format: lowercase alphanumeric with hyphens only
	validNamePattern := regexp.MustCompile(`^[a-z0-9-]+$`)
	if !validNamePattern.MatchString(p.Name) {
		return nil, fmt.Errorf("invalid provider name '%s': must contain only lowercase letters, numbers, and hyphens (e.g., 'twitter', 'azure-ad', 'google-workspace')", p.Name)
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

	// Check if a provider with this name already exists
	var existingID uuid.UUID
	checkQuery := `SELECT id FROM provider_profiles WHERE name = $1 AND deleted_at IS NULL LIMIT 1`
	err := s.db.QueryRow(checkQuery, p.Name).Scan(&existingID)
	if err == nil {
		// A provider with this name already exists
		return nil, fmt.Errorf("provider with name '%s' already exists", p.Name)
	}
	// If error is not "no rows", it's a real database error
	if err.Error() != "sql: no rows in result set" {
		return nil, fmt.Errorf("failed to check for existing provider: %w", err)
	}

	query := `
		INSERT INTO provider_profiles (name, client_id, client_secret, auth_url, token_url, issuer, enable_discovery, scopes, auth_type, auth_header, params)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id`

	var id uuid.UUID
	err = s.db.QueryRow(query, p.Name, p.ClientID, p.ClientSecret, p.AuthURL, p.TokenURL, p.Issuer, p.EnableDiscovery, pq.Array(p.Scopes), p.AuthType, p.AuthHeader, p.Params).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider profile: %w", err)
	}

	p.ID = id
	return &p, nil
}

// GetProfile retrieves a provider profile by ID
func (s *Store) GetProfile(id uuid.UUID) (*Profile, error) {
	var p Profile
	query := `SELECT id, name, client_id, client_secret, auth_url, token_url, issuer, enable_discovery, scopes, auth_type, auth_header, params FROM provider_profiles WHERE id = $1 AND deleted_at IS NULL`

	row := s.db.QueryRow(query, id)
	err := row.Scan(&p.ID, &p.Name, &p.ClientID, &p.ClientSecret, &p.AuthURL, &p.TokenURL, &p.Issuer, &p.EnableDiscovery, pq.Array(&p.Scopes), &p.AuthType, &p.AuthHeader, &p.Params)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider profile: %w", err)
	}

	return &p, nil
}

// GetProfileByName retrieves a provider profile by name
func (s *Store) GetProfileByName(name string) (*Profile, error) {
	var p Profile
	query := `SELECT id, name, client_id, client_secret, auth_url, token_url, issuer, enable_discovery, scopes, auth_type, auth_header, params FROM provider_profiles WHERE name = $1 AND deleted_at IS NULL`
	err := s.db.Get(&p, query, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider profile by name: %w", err)
	}
	return &p, nil
}

// UpdateProfile updates an existing provider profile
func (s *Store) UpdateProfile(p *Profile) error {
	query := `
		UPDATE provider_profiles
		SET
			name = $1,
			client_id = $2,
			client_secret = $3,
			auth_url = $4,
			token_url = $5,
			issuer = $6,
			enable_discovery = $7,
			scopes = $8,
			auth_type = $9,
			auth_header = $10,
			params = $11,
			updated_at = NOW()
		WHERE id = $12 AND deleted_at IS NULL`

	_, err := s.db.Exec(query, p.Name, p.ClientID, p.ClientSecret, p.AuthURL, p.TokenURL, p.Issuer, p.EnableDiscovery, pq.Array(p.Scopes), p.AuthType, p.AuthHeader, p.Params, p.ID)
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

// DeleteProfileByName soft-deletes ALL provider profiles with the given name
func (s *Store) DeleteProfileByName(name string) (int64, error) {
	query := `UPDATE provider_profiles SET deleted_at = NOW() WHERE name = $1 AND deleted_at IS NULL`
	result, err := s.db.Exec(query, name)
	if err != nil {
		return 0, fmt.Errorf("failed to delete provider profiles: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return rowsAffected, nil
}

// ListProfiles retrieves all non-deleted provider names and IDs
func (s *Store) ListProfiles() ([]ProfileList, error) {
	var rows []ProfileList
	query := `SELECT id, name FROM provider_profiles WHERE deleted_at IS NULL ORDER BY created_at DESC`
	if err := s.db.Select(&rows, query); err != nil {
		return nil, fmt.Errorf("failed to list profiles: %w", err)
	}
	return rows, nil
}
