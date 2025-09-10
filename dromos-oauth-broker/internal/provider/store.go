package provider

import (
	"encoding/json"
	"fmt"

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
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
	AuthURL      string    `json:"auth_url"`
	TokenURL     string    `json:"token_url"`
	Scopes       []string  `json:"scopes"`
}

// RegisterProfile registers a new provider profile from JSON
func (s *Store) RegisterProfile(profileJSON string) (*Profile, error) {
	var p Profile
	if err := json.Unmarshal([]byte(profileJSON), &p); err != nil {
		return nil, fmt.Errorf("invalid profile JSON: %w", err)
	}

	// Validate required fields
	if p.Name == "" || p.ClientID == "" || p.ClientSecret == "" || p.AuthURL == "" || p.TokenURL == "" {
		return nil, fmt.Errorf("missing required fields")
	}

	query := `
		INSERT INTO provider_profiles (name, client_id, client_secret, auth_url, token_url, scopes)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`

	var id uuid.UUID
	err := s.db.QueryRow(query, p.Name, p.ClientID, p.ClientSecret, p.AuthURL, p.TokenURL, p.Scopes).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider profile: %w", err)
	}

	p.ID = id
	return &p, nil
}

// GetProfile retrieves a provider profile by ID
func (s *Store) GetProfile(id uuid.UUID) (*Profile, error) {
	var p Profile
	query := `SELECT id, name, client_id, client_secret, auth_url, token_url, scopes FROM provider_profiles WHERE id = $1`

	row := s.db.QueryRow(query, id)
	err := row.Scan(&p.ID, &p.Name, &p.ClientID, &p.ClientSecret, &p.AuthURL, &p.TokenURL, &p.Scopes)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider profile: %w", err)
	}

	return &p, nil
}
