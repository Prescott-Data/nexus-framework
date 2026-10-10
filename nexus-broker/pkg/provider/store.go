package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/samlutil"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault/providers"
)

// Store provides provider profile management
type Store struct {
	db *sqlx.DB

	// secrets is where OAuth2 client secrets live. nil means the
	// provider_profiles.client_secret column (the internal backend).
	secrets providers.Backend
}

// Option configures a Store.
type Option func(*Store)

// WithSecretBackend moves OAuth2 client secrets out of the database into the
// given backend. Rows written while the backend is active hold NULL in
// client_secret; rows that still carry a column value keep working, so an
// existing deployment can switch backends without a data migration.
func WithSecretBackend(b providers.Backend) Option {
	return func(s *Store) { s.secrets = b }
}

// NewStore creates a new provider store
func NewStore(db *sqlx.DB, opts ...Option) *Store {
	s := &Store{db: db}
	for _, o := range opts {
		o(s)
	}
	return s
}

// secretBackendTimeout bounds each call to an external secret backend. Store
// methods have no context parameter, so the deadline is applied here.
const secretBackendTimeout = 10 * time.Second

func (s *Store) externalSecrets() bool { return s.secrets != nil }

func (s *Store) secretCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), secretBackendTimeout)
}

// resolveSecret fills p.ClientSecret from the external backend when the row
// holds NULL. A missing secret does not fail the read, so a provider whose
// secret was removed out of band can still be listed, inspected and
// re-registered; it is marked ClientSecretMissing so that execution paths and
// the health probe fail clearly instead of sending an empty secret.
func (s *Store) resolveSecret(p *Profile) error {
	if p.ClientSecret != nil {
		return nil
	}
	if p.AuthType != "" && p.AuthType != "oauth2" {
		return nil
	}
	if !s.externalSecrets() {
		// Registration has always required a client_secret for oauth2, so a
		// NULL column can only mean the row was written while an external
		// backend was active. Serving it with the internal backend would send
		// an empty secret to the provider; flag it so execution paths refuse.
		p.ClientSecretMissing = true
		return nil
	}
	ctx, cancel := s.secretCtx()
	defer cancel()
	v, err := s.secrets.Get(ctx, p.ID.String())
	if errors.Is(err, providers.ErrNotFound) {
		p.ClientSecretMissing = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: read client_secret for provider %s: %w", ErrSecretBackend, p.ID, err)
	}
	if strings.TrimSpace(v) == "" {
		// Changed out of band to a blank value; fail closed like a missing one.
		p.ClientSecretMissing = true
		return nil
	}
	p.ClientSecret = &v
	return nil
}

// withProviderLock runs fn inside a transaction that holds a Postgres
// advisory lock keyed by the provider id, so the backend write and the row
// update of one request cannot interleave with another request's across
// broker replicas. The lock is released when the transaction ends.
func (s *Store) withProviderLock(id uuid.UUID, fn func(tx *sqlx.Tx) error) error {
	tx, err := s.db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, id.String()); err != nil {
		return fmt.Errorf("failed to lock provider %s: %w", id, err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}
	return nil
}

func (s *Store) putSecret(id uuid.UUID, value string) error {
	ctx, cancel := s.secretCtx()
	defer cancel()
	if err := s.secrets.Set(ctx, id.String(), value); err != nil {
		return fmt.Errorf("%w: write client_secret for provider %s: %w", ErrSecretBackend, id, err)
	}
	return nil
}

func (s *Store) dropSecret(id uuid.UUID) error {
	ctx, cancel := s.secretCtx()
	defer cancel()
	if err := s.secrets.Delete(ctx, id.String()); err != nil {
		return fmt.Errorf("%w: delete client_secret for provider %s: %w", ErrSecretBackend, id, err)
	}
	return nil
}

// Profile represents a provider profile
type Profile struct {
	ID                uuid.UUID        `json:"id" db:"id"`
	Name              string           `json:"name" db:"name"`
	Description       string           `json:"description,omitempty" db:"description"`
	Category          string           `json:"category,omitempty" db:"category"`
	AuthType          string           `json:"auth_type,omitempty" db:"auth_type"`
	AuthHeader        string           `json:"auth_header,omitempty" db:"auth_header"`
	ClientID          *string          `json:"client_id,omitempty" db:"client_id"`
	ClientSecret      *string          `json:"client_secret,omitempty" db:"client_secret"`
	AuthURL           *string          `json:"auth_url,omitempty" db:"auth_url"`
	TokenURL          *string          `json:"token_url,omitempty" db:"token_url"`
	Issuer            *string          `json:"issuer,omitempty" db:"issuer"`
	EnableDiscovery   bool             `json:"enable_discovery" db:"enable_discovery"`
	Scopes            []string         `json:"scopes" db:"scopes"`
	APIBaseURL        string           `json:"api_base_url,omitempty" db:"api_base_url"`
	UserInfoEndpoint  string           `json:"user_info_endpoint,omitempty" db:"user_info_endpoint"`
	Params            *json.RawMessage `json:"params,omitempty" db:"params"`
	SAMLIdpEntityID   *string          `json:"saml_idp_entity_id,omitempty" db:"saml_idp_entity_id"`
	SAMLIdpSSOURL     *string          `json:"saml_idp_sso_url,omitempty" db:"saml_idp_sso_url"`
	SAMLIdpX509Cert   *string          `json:"saml_idp_x509_cert,omitempty" db:"saml_idp_x509_cert"`
	SAMLSPEntityID    *string          `json:"saml_sp_entity_id,omitempty" db:"saml_sp_entity_id"`
	DeletedAt         *time.Time       `json:"-" db:"deleted_at"`
	LastHealthCheckAt *time.Time       `json:"last_health_check_at,omitempty" db:"last_health_check_at"`
	HealthStatus      string           `json:"health_status" db:"health_status"`
	HealthMessage     *string          `json:"health_message,omitempty" db:"health_message"`

	// ClientSecretMissing is set when an external secret backend is active,
	// the row holds no client_secret, and the backend has no secret for this
	// provider either. Metadata reads still succeed; OAuth exchange, refresh,
	// revocation and the health probe must refuse rather than send an empty
	// secret to the provider.
	ClientSecretMissing bool `json:"-" db:"-"`
}

// ErrInvalidProfile marks a caller error in an update or patch payload, so
// handlers can answer 400 rather than 500.
var ErrInvalidProfile = errors.New("invalid provider profile")

// ErrProviderNotFound is returned when an update or patch matched no active
// provider row, so handlers can answer 404.
var ErrProviderNotFound = errors.New("provider not found")

// ErrSecretBackend marks an operational failure talking to the external
// secret backend (timeout, permission, API error). Handlers map it to a
// sanitized 503 rather than a 400 or 404, since the request may succeed on
// retry and the underlying cloud error must not reach the client.
var ErrSecretBackend = errors.New("secret backend unavailable")

// ErrClientSecretMissing is returned by code paths that need a client secret
// for a provider whose ClientSecretMissing flag is set.
var ErrClientSecretMissing = errors.New("provider client_secret is missing from the secret backend")

// RequireClientSecret returns the client secret or ErrClientSecretMissing.
// Call it on every path that sends the secret to the provider.
func (p *Profile) RequireClientSecret() (string, error) {
	if p.ClientSecretMissing {
		return "", ErrClientSecretMissing
	}
	if p.ClientSecret == nil {
		return "", nil
	}
	return *p.ClientSecret, nil
}

// Redacted returns a copy of the profile with sensitive fields removed so it is
// safe to serialize in API responses. The client_secret must never leave the
// broker; internal OAuth flows read the secret directly from the store, not via
// HTTP responses.
func (p *Profile) Redacted() *Profile {
	if p == nil {
		return nil
	}
	clone := *p
	clone.ClientSecret = nil
	return &clone
}

// RegisterProfile registers a new provider profile from JSON
func (s *Store) RegisterProfile(profileJSON string) (*Profile, error) {
	var p Profile
	if err := json.Unmarshal([]byte(profileJSON), &p); err != nil {
		return nil, fmt.Errorf("profile: invalid JSON: %w", err)
	}

	// Validate provider name format: lowercase letters, numbers, hyphens
	validNamePattern := regexp.MustCompile(`^[a-z0-9-]+$`)
	if p.Name == "" {
		return nil, fmt.Errorf("name: missing required field")
	}
	if !validNamePattern.MatchString(p.Name) {
		return nil, fmt.Errorf("name: invalid provider name '%s', must contain only lowercase letters, numbers, and hyphens", p.Name)
	}

	// Validate fields based on auth type
	switch p.AuthType {
	case "oauth2", "": // Default oauth2
		if p.ClientID == nil || *p.ClientID == "" {
			return nil, fmt.Errorf("client_id: missing required field")
		}
		if p.ClientSecret == nil || strings.TrimSpace(*p.ClientSecret) == "" {
			return nil, fmt.Errorf("client_secret: missing required field")
		}

		if p.EnableDiscovery {
			// When discovery is enabled, issuer must be set
			if p.Issuer == nil || strings.TrimSpace(*p.Issuer) == "" {
				return nil, fmt.Errorf("issuer: required when enable_discovery is true")
			}
			// Explicitly skip auth_url/token_url validation when discovery is enabled
		} else {
			// When discovery is disabled, auth_url and token_url must be set
			// But also check: if issuer is provided without URLs, they probably meant to enable discovery
			if p.Issuer != nil && *p.Issuer != "" && (p.AuthURL == nil || *p.AuthURL == "") && (p.TokenURL == nil || *p.TokenURL == "") {
				return nil, fmt.Errorf("enable_discovery: must be set to true when using issuer without auth_url/token_url")
			}

			if p.AuthURL == nil || *p.AuthURL == "" {
				return nil, fmt.Errorf("auth_url: missing required field")
			}
			if p.TokenURL == nil || *p.TokenURL == "" {
				return nil, fmt.Errorf("token_url: missing required field")
			}
		}

	case "saml":
		if err := validateSAMLProfile(p); err != nil {
			return nil, err
		}

	case "api_key", "basic_auth", "header", "query_param", "hmac_payload", "aws_sigv4":
		// Only name is required for static auth types

	default:
		return nil, fmt.Errorf("auth_type: unsupported value '%s'", p.AuthType)
	}

	// Check for duplicate provider
	var existingID uuid.UUID
	checkQuery := `SELECT id FROM provider_profiles WHERE name = $1 AND deleted_at IS NULL LIMIT 1`
	err := s.db.QueryRow(checkQuery, p.Name).Scan(&existingID)
	if err == nil {
		return nil, fmt.Errorf("name: provider with name '%s' already exists", p.Name)
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("database: error checking provider existence: %w", err)
	}

	// Normalize values for DB insertion - use nil for NULL
	var issuer interface{}
	if p.Issuer != nil {
		issuer = *p.Issuer
	}

	var authURL, tokenURL interface{}
	if p.EnableDiscovery {
		authURL = nil
		tokenURL = nil
	} else {
		authURL = p.AuthURL
		tokenURL = p.TokenURL
	}

	scopes := pq.Array(p.Scopes)
	if p.Scopes == nil {
		scopes = pq.Array([]string{})
	}

	// Insert into DB
	query := `
		INSERT INTO provider_profiles
		(name, client_id, client_secret, auth_url, token_url, issuer, enable_discovery, scopes, auth_type, auth_header, api_base_url, user_info_endpoint, params, description, category, saml_idp_entity_id, saml_idp_sso_url, saml_idp_x509_cert, saml_sp_entity_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING id`

	if !s.externalSecrets() || p.ClientSecret == nil {
		var id uuid.UUID
		err = s.db.QueryRow(query,
			p.Name, p.ClientID, p.ClientSecret, authURL, tokenURL, issuer,
			p.EnableDiscovery, scopes, p.AuthType, p.AuthHeader,
			p.APIBaseURL, p.UserInfoEndpoint, p.Params, p.Description, p.Category,
			p.SAMLIdpEntityID, p.SAMLIdpSSOURL, p.SAMLIdpX509Cert, p.SAMLSPEntityID,
		).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("database: failed to create provider profile: %w", err)
		}
		p.ID = id
		return &p, nil
	}

	// External backend: the id is chosen here so the secret can be written
	// under it before the row is published. The insert stays uncommitted
	// until the backend write succeeds, so no reader ever sees a row whose
	// secret does not exist yet, and a crash in between leaves no row behind.
	id := uuid.New()
	query = strings.Replace(query, "(name, client_id, client_secret,", "(id, name, client_id, client_secret,", 1)
	query = strings.Replace(query, "VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)", "VALUES ($20,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)", 1)

	tx, err := s.db.Beginx()
	if err != nil {
		return nil, fmt.Errorf("database: failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var inserted uuid.UUID
	err = tx.QueryRow(query,
		p.Name, p.ClientID, nil, authURL, tokenURL, issuer,
		p.EnableDiscovery, scopes, p.AuthType, p.AuthHeader,
		p.APIBaseURL, p.UserInfoEndpoint, p.Params, p.Description, p.Category,
		p.SAMLIdpEntityID, p.SAMLIdpSSOURL, p.SAMLIdpX509Cert, p.SAMLSPEntityID,
		id,
	).Scan(&inserted)
	if err != nil {
		return nil, fmt.Errorf("database: failed to create provider profile: %w", err)
	}

	if err := s.putSecret(id, *p.ClientSecret); err != nil {
		// The row was never committed; only the secret may exist part way.
		if dropErr := s.dropSecret(id); dropErr != nil {
			return nil, fmt.Errorf("%w (cleanup also failed: %w)", err, dropErr)
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		// A commit error can have an unknown outcome (committed, then the
		// acknowledgement was lost). Only drop the secret once the database
		// confirms the row is absent; if it exists, or cannot be checked,
		// keep the secret so a committed provider is never left without it.
		var exists bool
		checkErr := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM provider_profiles WHERE id = $1)`, id).Scan(&exists)
		switch {
		case checkErr != nil:
			return nil, fmt.Errorf("database: failed to commit provider profile: %w (outcome unknown, secret %s kept: %w)", err, id, checkErr)
		case exists:
			return nil, fmt.Errorf("database: commit reported an error but provider %s exists; its secret is in place: %w", id, err)
		}
		if dropErr := s.dropSecret(id); dropErr != nil {
			return nil, fmt.Errorf("database: failed to commit provider profile: %w (cleanup also failed: %w)", err, dropErr)
		}
		return nil, fmt.Errorf("database: failed to commit provider profile: %w", err)
	}

	p.ID = inserted
	return &p, nil
}

// GetProfile retrieves a provider profile by ID
func (s *Store) GetProfile(id uuid.UUID) (*Profile, error) {
	var p Profile
	query := `SELECT id, name, client_id, client_secret, auth_url, token_url, issuer, enable_discovery, scopes, auth_type, COALESCE(auth_header, ''), COALESCE(api_base_url, ''), COALESCE(user_info_endpoint, ''), params, COALESCE(description, ''), COALESCE(category, ''), last_health_check_at, COALESCE(health_status, 'unknown'), health_message, saml_idp_entity_id, saml_idp_sso_url, saml_idp_x509_cert, saml_sp_entity_id FROM provider_profiles WHERE id = $1 AND deleted_at IS NULL`

	row := s.db.QueryRow(query, id)
	err := row.Scan(&p.ID, &p.Name, &p.ClientID, &p.ClientSecret, &p.AuthURL, &p.TokenURL, &p.Issuer, &p.EnableDiscovery, pq.Array(&p.Scopes), &p.AuthType, &p.AuthHeader, &p.APIBaseURL, &p.UserInfoEndpoint, &p.Params, &p.Description, &p.Category, &p.LastHealthCheckAt, &p.HealthStatus, &p.HealthMessage, &p.SAMLIdpEntityID, &p.SAMLIdpSSOURL, &p.SAMLIdpX509Cert, &p.SAMLSPEntityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider profile: %w", err)
	}
	if err := s.resolveSecret(&p); err != nil {
		return nil, err
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
		       COALESCE(api_base_url, ''), COALESCE(user_info_endpoint, ''), params,
		       COALESCE(description, ''), COALESCE(category, ''), last_health_check_at,
		       COALESCE(health_status, 'unknown'), health_message,
	       saml_idp_entity_id, saml_idp_sso_url, saml_idp_x509_cert, saml_sp_entity_id
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
			&p.AuthHeader, &p.APIBaseURL, &p.UserInfoEndpoint, &p.Params, &p.Description, &p.Category,
			&p.LastHealthCheckAt, &p.HealthStatus, &p.HealthMessage,
			&p.SAMLIdpEntityID, &p.SAMLIdpSSOURL, &p.SAMLIdpX509Cert, &p.SAMLSPEntityID,
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
	if err := s.resolveSecret(&profiles[0]); err != nil {
		return nil, err
	}
	return &profiles[0], nil
}

// UpdateProfile updates an existing provider profile
func (s *Store) UpdateProfile(p *Profile) error {
	if p == nil {
		return fmt.Errorf("provider profile is required")
	}
	if p.AuthType == "saml" {
		if err := validateSAMLProfile(*p); err != nil {
			return err
		}
	}

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
			description = $14,
			category = $15,
			saml_idp_entity_id = $16,
			saml_idp_sso_url = $17,
			saml_idp_x509_cert = $18,
			saml_sp_entity_id = $19,
			updated_at = NOW()
		WHERE id = $20 AND deleted_at IS NULL`

	args := func(secretArg interface{}) []interface{} {
		return []interface{}{p.Name, p.ClientID, secretArg, p.AuthURL, p.TokenURL, p.Issuer, p.EnableDiscovery, pq.Array(p.Scopes), p.AuthType, p.AuthHeader, p.APIBaseURL, p.UserInfoEndpoint, p.Params, p.Description, p.Category, p.SAMLIdpEntityID, p.SAMLIdpSSOURL, p.SAMLIdpX509Cert, p.SAMLSPEntityID, p.ID}
	}

	if !s.externalSecrets() {
		if _, err := s.db.Exec(query, args(p.ClientSecret)...); err != nil {
			return fmt.Errorf("failed to update provider profile: %w", err)
		}
		return nil
	}

	// With an external backend the column is only ever cleared, and only
	// when a new secret was written to the backend. A PUT that omits
	// client_secret keeps whatever is stored, in the backend or, for a row
	// written before the switch, still in the column. Clearing a secret is
	// done by deleting the provider, not by PUT.
	query = strings.Replace(query, "client_secret = $3,", "client_secret = CASE WHEN $3::boolean THEN NULL ELSE client_secret END,", 1)
	if p.ClientSecret == nil {
		if _, err := s.db.Exec(query, args(false)...); err != nil {
			return fmt.Errorf("failed to update provider profile: %w", err)
		}
		return nil
	}
	if strings.TrimSpace(*p.ClientSecret) == "" {
		return fmt.Errorf("%w: client_secret: must not be empty; delete the provider to clear its secret", ErrInvalidProfile)
	}

	// A rotation writes the backend and the row under one per-provider lock,
	// so two concurrent updates cannot leave one request's metadata paired
	// with the other's secret.
	return s.withProviderLock(p.ID, func(tx *sqlx.Tx) error {
		if err := s.putSecret(p.ID, *p.ClientSecret); err != nil {
			return err
		}
		result, err := tx.Exec(query, args(true)...)
		if err != nil {
			return fmt.Errorf("failed to update provider profile: %w", err)
		}
		return s.ensureRowMatched(result, p.ID)
	})
}

// ensureRowMatched guards the write paths that put a secret in the backend
// before touching the row. When the UPDATE matched no active row, the id is
// unknown or soft-deleted, so the secret just written is removed again and
// the caller gets a not-found error instead of a silently orphaned secret.
func (s *Store) ensureRowMatched(result sql.Result, id uuid.UUID) error {
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if n > 0 {
		return nil
	}
	if dropErr := s.dropSecret(id); dropErr != nil {
		return fmt.Errorf("%w: %s (and failed to remove its secret: %w)", ErrProviderNotFound, id, dropErr)
	}
	return fmt.Errorf("%w: %s", ErrProviderNotFound, id)
}

// PatchProfile updates specific fields of a provider profile
func (s *Store) PatchProfile(id uuid.UUID, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}
	if err := s.validateSAMLProfilePatch(id, updates); err != nil {
		return err
	}

	query := "UPDATE provider_profiles SET "
	args := []interface{}{}
	i := 1
	secretWritten := false
	newSecret := ""

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
			if s.externalSecrets() {
				secret, ok := value.(string)
				if !ok {
					return fmt.Errorf("%w: client_secret: must be a string", ErrInvalidProfile)
				}
				if strings.TrimSpace(secret) == "" {
					return fmt.Errorf("%w: client_secret: must not be empty; delete the provider to clear its secret", ErrInvalidProfile)
				}
				newSecret = secret
				secretWritten = true
				value = nil
			}
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
		case "description":
			column = "description"
		case "category":
			column = "category"
		case "saml_idp_entity_id":
			column = "saml_idp_entity_id"
		case "saml_idp_sso_url":
			column = "saml_idp_sso_url"
		case "saml_idp_x509_cert":
			column = "saml_idp_x509_cert"
		case "saml_sp_entity_id":
			column = "saml_sp_entity_id"
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

	if !secretWritten {
		if _, err := s.db.Exec(query, args...); err != nil {
			return fmt.Errorf("failed to patch provider profile: %w", err)
		}
		return nil
	}

	// Backend write and row update under one per-provider lock; see UpdateProfile.
	return s.withProviderLock(id, func(tx *sqlx.Tx) error {
		if err := s.putSecret(id, newSecret); err != nil {
			return err
		}
		result, err := tx.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("failed to patch provider profile: %w", err)
		}
		return s.ensureRowMatched(result, id)
	})
}

// DeleteProfile soft-deletes a provider profile by ID
func (s *Store) DeleteProfile(id uuid.UUID) error {
	query := `UPDATE provider_profiles SET deleted_at = NOW() WHERE id = $1`
	if s.externalSecrets() {
		// A row written before the switch may still hold its secret in the
		// column; a soft-deleted row must not keep it.
		query = `UPDATE provider_profiles SET deleted_at = NOW(), client_secret = NULL WHERE id = $1`
	}
	_, err := s.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete provider profile: %w", err)
	}
	if s.externalSecrets() {
		return s.dropSecret(id)
	}
	return nil
}

// DeleteProfileByName soft-deletes ALL provider profiles with the given name
func (s *Store) DeleteProfileByName(name string) (int64, error) {
	// The soft delete returns the ids it touched, so a row registered
	// concurrently under the same name is either deleted here and its secret
	// dropped below, or left alone entirely; never deleted with its secret
	// orphaned.
	query := `UPDATE provider_profiles SET deleted_at = NOW() WHERE name = $1 AND deleted_at IS NULL RETURNING id`
	if s.externalSecrets() {
		query = `UPDATE provider_profiles SET deleted_at = NOW(), client_secret = NULL WHERE name = $1 AND deleted_at IS NULL RETURNING id`
	}
	var deleted []uuid.UUID
	if err := s.db.Select(&deleted, query, name); err != nil {
		return 0, fmt.Errorf("failed to delete provider profiles: %w", err)
	}
	rowsAffected := int64(len(deleted))

	if !s.externalSecrets() {
		return rowsAffected, nil
	}

	// Rows soft-deleted earlier whose secret may still exist after a backend
	// failure on a previous attempt, so a retry cleans up.
	var historical []uuid.UUID
	if err := s.db.Select(&historical, `SELECT id FROM provider_profiles WHERE name = $1 AND deleted_at IS NOT NULL`, name); err != nil {
		return rowsAffected, fmt.Errorf("failed to look up deleted provider profiles: %w", err)
	}

	seen := make(map[uuid.UUID]struct{}, len(deleted)+len(historical))
	var dropErrs []error
	for _, id := range append(deleted, historical...) {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if err := s.dropSecret(id); err != nil {
			dropErrs = append(dropErrs, err)
		}
	}
	if len(dropErrs) > 0 {
		return rowsAffected, errors.Join(dropErrs...)
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

// GetAllProfiles retrieves all non-deleted provider profiles in full
func (s *Store) GetAllProfiles() ([]Profile, error) {
	query := `
		SELECT id, name, client_id, client_secret, auth_url, token_url, issuer,
		       enable_discovery, scopes, auth_type, COALESCE(auth_header, ''),
		       COALESCE(api_base_url, ''), COALESCE(user_info_endpoint, ''), params,
		       COALESCE(description, ''), COALESCE(category, ''), last_health_check_at,
		       COALESCE(health_status, 'unknown'), health_message,
	       saml_idp_entity_id, saml_idp_sso_url, saml_idp_x509_cert, saml_sp_entity_id
		FROM provider_profiles
		WHERE deleted_at IS NULL
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all profiles: %w", err)
	}
	defer rows.Close()

	var profiles []Profile
	for rows.Next() {
		var p Profile
		err := rows.Scan(
			&p.ID, &p.Name, &p.ClientID, &p.ClientSecret, &p.AuthURL, &p.TokenURL,
			&p.Issuer, &p.EnableDiscovery, pq.Array(&p.Scopes), &p.AuthType,
			&p.AuthHeader, &p.APIBaseURL, &p.UserInfoEndpoint, &p.Params, &p.Description, &p.Category,
			&p.LastHealthCheckAt, &p.HealthStatus, &p.HealthMessage,
			&p.SAMLIdpEntityID, &p.SAMLIdpSSOURL, &p.SAMLIdpX509Cert, &p.SAMLSPEntityID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan provider profile: %w", err)
		}
		profiles = append(profiles, p)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating all profiles: %w", err)
	}

	if err := s.resolveSecrets(profiles); err != nil {
		return nil, err
	}

	return profiles, nil
}

// resolveSecretsConcurrency bounds parallel backend reads in GetAllProfiles,
// so one slow backend cannot turn a pass over N providers into N timeouts
// in series. It matches the health worker's own concurrency limit.
const resolveSecretsConcurrency = 10

func (s *Store) resolveSecrets(profiles []Profile) error {
	if !s.externalSecrets() {
		for i := range profiles {
			if err := s.resolveSecret(&profiles[i]); err != nil {
				return err
			}
		}
		return nil
	}

	sem := make(chan struct{}, resolveSecretsConcurrency)
	errs := make([]error, len(profiles))
	var wg sync.WaitGroup
	for i := range profiles {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			errs[i] = s.resolveSecret(&profiles[i])
		}(i)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// UpdateHealthStatus updates the health fields for a given provider profile
func (s *Store) UpdateHealthStatus(id uuid.UUID, status string, message *string) error {
	query := `UPDATE provider_profiles SET health_status = $1, health_message = $2, last_health_check_at = NOW() WHERE id = $3 AND deleted_at IS NULL`
	_, err := s.db.Exec(query, status, message, id)
	if err != nil {
		return fmt.Errorf("failed to update provider health status: %w", err)
	}
	return nil
}

// GetHealthStatus returns only the health_status for a provider.
// This is a narrow query intended for background workers that need to
// cross-reference provider health without loading sensitive fields.
func (s *Store) GetHealthStatus(id uuid.UUID) (string, error) {
	var status string
	err := s.db.QueryRow(
		`SELECT COALESCE(health_status, 'unknown') FROM provider_profiles WHERE id = $1 AND deleted_at IS NULL`,
		id,
	).Scan(&status)
	if err != nil {
		return "", fmt.Errorf("failed to get provider health status: %w", err)
	}
	return status, nil
}

// ProviderHealthSummary is a lightweight view containing only health-related fields.
// Used by the /providers/health endpoint to avoid loading sensitive columns.
type ProviderHealthSummary struct {
	ID                uuid.UUID  `json:"id"`
	Name              string     `json:"name"`
	HealthStatus      string     `json:"health_status"`
	LastHealthCheckAt *time.Time `json:"last_health_check_at,omitempty"`
	HealthMessage     *string    `json:"health_message,omitempty"`
}

// GetAllHealthStatuses returns health-only summaries for all active providers.
// This is a narrow query that avoids selecting sensitive fields (client_secret, params, etc.).
func (s *Store) GetAllHealthStatuses() ([]ProviderHealthSummary, error) {
	query := `
		SELECT id, name, COALESCE(health_status, 'unknown'), last_health_check_at, health_message
		FROM provider_profiles
		WHERE deleted_at IS NULL
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query provider health statuses: %w", err)
	}
	defer rows.Close()

	var summaries []ProviderHealthSummary
	for rows.Next() {
		var s ProviderHealthSummary
		if err := rows.Scan(&s.ID, &s.Name, &s.HealthStatus, &s.LastHealthCheckAt, &s.HealthMessage); err != nil {
			return nil, fmt.Errorf("failed to scan provider health summary: %w", err)
		}
		summaries = append(summaries, s)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating provider health statuses: %w", err)
	}

	// Return empty slice instead of nil for clean JSON serialization
	if summaries == nil {
		summaries = []ProviderHealthSummary{}
	}

	return summaries, nil
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
			scopes,
			COALESCE(description, '') as description,
			COALESCE(category, '') as category
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
		var name, authType, apiBaseURL, userInfoEndpoint, description, category string
		var scopes []string

		// auth_type usually defaults to 'oauth2' if empty in some contexts,
		// but here we trust the DB value.
		if err := rows.Scan(&id, &name, &authType, &apiBaseURL, &userInfoEndpoint, pq.Array(&scopes), &description, &category); err != nil {
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
			"description":        description,
			"category":           category,
		}
	}

	return result, nil
}

func (s *Store) validateSAMLProfilePatch(id uuid.UUID, updates map[string]interface{}) error {
	if !touchesSAMLProfile(updates) {
		return nil
	}

	current, err := s.GetProfile(id)
	if err != nil {
		return fmt.Errorf("failed to load provider profile for patch validation: %w", err)
	}
	merged := *current

	for key, value := range updates {
		switch key {
		case "auth_type":
			authType, ok := value.(string)
			if !ok {
				return fmt.Errorf("auth_type: must be a string")
			}
			merged.AuthType = authType
		case "saml_idp_entity_id":
			v, err := patchStringPointer(key, value)
			if err != nil {
				return err
			}
			merged.SAMLIdpEntityID = v
		case "saml_idp_sso_url":
			v, err := patchStringPointer(key, value)
			if err != nil {
				return err
			}
			merged.SAMLIdpSSOURL = v
		case "saml_idp_x509_cert":
			v, err := patchStringPointer(key, value)
			if err != nil {
				return err
			}
			merged.SAMLIdpX509Cert = v
		case "saml_sp_entity_id":
			v, err := patchStringPointer(key, value)
			if err != nil {
				return err
			}
			merged.SAMLSPEntityID = v
		}
	}

	if merged.AuthType != "saml" {
		return nil
	}
	return validateSAMLProfile(merged)
}

func touchesSAMLProfile(updates map[string]interface{}) bool {
	for key := range updates {
		switch key {
		case "auth_type", "saml_idp_entity_id", "saml_idp_sso_url", "saml_idp_x509_cert", "saml_sp_entity_id":
			return true
		}
	}
	return false
}

func patchStringPointer(field string, value interface{}) (*string, error) {
	if value == nil {
		return nil, nil
	}
	str, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("%s: must be a string", field)
	}
	return &str, nil
}
func validateSAMLProfile(p Profile) error {
	idpEntityID := strings.TrimSpace(stringValue(p.SAMLIdpEntityID))
	idpSSOURL := strings.TrimSpace(stringValue(p.SAMLIdpSSOURL))
	idpCert := strings.TrimSpace(stringValue(p.SAMLIdpX509Cert))
	spEntityID := strings.TrimSpace(stringValue(p.SAMLSPEntityID))

	switch {
	case idpEntityID == "":
		return fmt.Errorf("saml_idp_entity_id: missing required field for saml")
	case idpSSOURL == "":
		return fmt.Errorf("saml_idp_sso_url: missing required field for saml")
	case idpCert == "":
		return fmt.Errorf("saml_idp_x509_cert: missing required field for saml")
	case spEntityID == "":
		return fmt.Errorf("saml_sp_entity_id: missing required field for saml")
	}

	if err := samlutil.ValidateProviderConfig(idpEntityID, idpSSOURL, idpCert, spEntityID); err != nil {
		return fmt.Errorf("saml: %w", err)
	}
	return nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
