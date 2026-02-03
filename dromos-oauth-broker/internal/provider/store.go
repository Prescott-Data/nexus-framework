package provider

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
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
	ID               uuid.UUID        `json:"id" db:"id"`
	Name             string           `json:"name" db:"name"`
	AuthType         string           `json:"auth_type,omitempty" db:"auth_type"`
	AuthHeader       string           `json:"auth_header,omitempty" db:"auth_header"`
	ClientID         string           `json:"client_id,omitempty" db:"client_id"`
	ClientSecret     string           `json:"client_secret,omitempty" db:"client_secret"`
	AuthURL          string           `json:"auth_url,omitempty" db:"auth_url"`
	TokenURL         string           `json:"token_url,omitempty" db:"token_url"`
	Issuer           *string          `json:"issuer,omitempty" db:"issuer"`
	EnableDiscovery  bool             `json:"enable_discovery" db:"enable_discovery"`
	Scopes           []string         `json:"scopes" db:"scopes"`
	APIBaseURL       string           `json:"api_base_url,omitempty" db:"api_base_url"`
	UserInfoEndpoint string           `json:"user_info_endpoint,omitempty" db:"user_info_endpoint"`
	Params           *json.RawMessage `json:"params,omitempty" db:"params"`
	DeletedAt        *time.Time       `json:"-" db:"deleted_at"`
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
	case "api_key", "basic_auth", "header", "query_param", "hmac_payload", "aws_sigv4":
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
		INSERT INTO provider_profiles (name, client_id, client_secret, auth_url, token_url, issuer, enable_discovery, scopes, auth_type, auth_header, api_base_url, user_info_endpoint, params)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id`

	var id uuid.UUID
	err = s.db.QueryRow(query, p.Name, p.ClientID, p.ClientSecret, p.AuthURL, p.TokenURL, p.Issuer, p.EnableDiscovery, pq.Array(p.Scopes), p.AuthType, p.AuthHeader, p.APIBaseURL, p.UserInfoEndpoint, p.Params).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider profile: %w", err)
	}

	p.ID = id
	return &p, nil
}

// GetProfile retrieves a provider profile by ID
func (s *Store) GetProfile(id uuid.UUID) (*Profile, error) {
	var p Profile
	query := `SELECT id, name, client_id, client_secret, auth_url, token_url, issuer, enable_discovery, scopes, auth_type, COALESCE(auth_header, ''), COALESCE(api_base_url, ''), COALESCE(user_info_endpoint, ''), params FROM provider_profiles WHERE id = $1 AND deleted_at IS NULL`

	row := s.db.QueryRow(query, id)
	err := row.Scan(&p.ID, &p.Name, &p.ClientID, &p.ClientSecret, &p.AuthURL, &p.TokenURL, &p.Issuer, &p.EnableDiscovery, pq.Array(&p.Scopes), &p.AuthType, &p.AuthHeader, &p.APIBaseURL, &p.UserInfoEndpoint, &p.Params)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider profile: %w", err)
	}

	return &p, nil
}

// GetProfileByName retrieves a provider profile by name
func (s *Store) GetProfileByName(name string) (*Profile, error) {
	// Normalize input to lowercase
	nameLower := strings.ToLower(name)

	// Use LOWER(name) in SQL for case-insensitive match
	query := `
		SELECT id, name, client_id, client_secret, auth_url, token_url, issuer,
		       enable_discovery, scopes, auth_type, COALESCE(auth_header, ''),
		       COALESCE(api_base_url, ''), COALESCE(user_info_endpoint, ''), params
		FROM provider_profiles
		WHERE LOWER(name) = $1 AND deleted_at IS NULL
	`

	rows, err := s.db.Query(query, nameLower)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider profile by name: %w", err)
	}
	defer rows.Close()

	var profiles []Profile
	for rows.Next() {
		var p Profile
		err := rows.Scan(
			&p.ID, &p.Name, &p.ClientID, &p.ClientSecret, &p.AuthURL, &p.TokenURL,
			&p.Issuer, &p.EnableDiscovery, pq.Array(&p.Scopes), &p.AuthType,
			&p.AuthHeader, &p.APIBaseURL, &p.UserInfoEndpoint, &p.Params,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan provider profile: %w", err)
		}
		profiles = append(profiles, p)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating provider profiles: %w", err)
	}

	if len(profiles) == 0 {
		return nil, fmt.Errorf("provider '%s' not found", name)
	}
	if len(profiles) > 1 {
		return nil, fmt.Errorf(
			"multiple providers found with name '%s' (found %d) - database integrity issue, please contact administrator",
			name, len(profiles),
		)
	}
	return &profiles[0], nil
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
			api_base_url = $11,
			user_info_endpoint = $12,
			params = $13,
			updated_at = NOW()
		WHERE id = $14 AND deleted_at IS NULL`

	_, err := s.db.Exec(query, p.Name, p.ClientID, p.ClientSecret, p.AuthURL, p.TokenURL, p.Issuer, p.EnableDiscovery, pq.Array(p.Scopes), p.AuthType, p.AuthHeader, p.APIBaseURL, p.UserInfoEndpoint, p.Params, p.ID)
	if err != nil {
		return fmt.Errorf("failed to update provider profile: %w", err)
	}

	return nil
}

// PatchProfile updates specific fields of a provider profile
func (s *Store) PatchProfile(id uuid.UUID, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	query := "UPDATE provider_profiles SET "
	args := []interface{}{}
	i := 1

	for key, value := range updates {
		// Whitelist all allowed columns and map them to snake_case if coming from JSON
		var column string
		switch key {
		case "name":
			column = "name"
		case "client_id":
			column = "client_id"
		case "client_secret":
			column = "client_secret"
		case "auth_url":
			column = "auth_url"
		case "token_url":
			column = "token_url"
		case "issuer":
			column = "issuer"
		case "enable_discovery":
			column = "enable_discovery"
		case "scopes":
			column = "scopes"
			// Handle array conversion for pq
			if slice, ok := value.([]interface{}); ok {
				strSlice := make([]string, len(slice))
				for j, v := range slice {
					strSlice[j] = fmt.Sprint(v)
				}
				value = pq.Array(strSlice)
			} else if slice, ok := value.([]string); ok {
				value = pq.Array(slice)
			}
		case "auth_type":
			column = "auth_type"
		case "auth_header":
			column = "auth_header"
		case "api_base_url":
			column = "api_base_url"
		case "user_info_endpoint":
			column = "user_info_endpoint"
		case "params":
			column = "params"
			// Handle JSON RawMessage or map conversion if needed
			if m, ok := value.(map[string]interface{}); ok {
				b, _ := json.Marshal(m)
				value = b
			}
		default:
			// Ignore unknown fields
			continue
		}

		if i > 1 {
			query += ", "
		}
		query += fmt.Sprintf("%s = $%d", column, i)
		args = append(args, value)
		i++
	}

	// Always update updated_at
	if i > 1 {
		query += ", "
	}
	query += "updated_at = NOW()"

	query += fmt.Sprintf(" WHERE id = $%d AND deleted_at IS NULL", i)
	args = append(args, id)

	_, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to patch provider profile: %w", err)
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

// GetMetadata retrieves integration metadata for all providers, grouped by auth_type
func (s *Store) GetMetadata() (map[string]map[string]interface{}, error) {
	query := `
		SELECT 
			id,
			name, 
			auth_type, 
			COALESCE(api_base_url, '') as api_base_url, 
			COALESCE(user_info_endpoint, '') as user_info_endpoint, 
			scopes 
		FROM provider_profiles 
		WHERE deleted_at IS NULL
		ORDER BY name`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query metadata: %w", err)
	}
	defer rows.Close()

	result := make(map[string]map[string]interface{})

	for rows.Next() {
		var id uuid.UUID
		var name, authType, apiBaseURL, userInfoEndpoint string
		var scopes []string

		// auth_type usually defaults to 'oauth2' if empty in some contexts,
		// but here we trust the DB value.
		if err := rows.Scan(&id, &name, &authType, &apiBaseURL, &userInfoEndpoint, pq.Array(&scopes)); err != nil {
			return nil, fmt.Errorf("failed to scan metadata: %w", err)
		}

		if authType == "" {
			authType = "oauth2" // Default fallback
		}

		if _, ok := result[authType]; !ok {
			result[authType] = make(map[string]interface{})
		}

		result[authType][name] = map[string]interface{}{
			"id":                 id.String(),
			"api_base_url":       apiBaseURL,
			"user_info_endpoint": userInfoEndpoint,
			"scopes":             scopes,
		}
	}

	return result, nil
}
