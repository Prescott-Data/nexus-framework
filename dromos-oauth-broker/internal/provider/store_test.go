package provider

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

func TestRegisterProfile_OAuth2(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	// Duplicate check: no rows found
	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name = \$1`).
		WithArgs("test-oauth2-provider").
		WillReturnError(sql.ErrNoRows)

	// Mock INSERT query
	rows := sqlmock.NewRows([]string{"id"}).AddRow("a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0a0")
	mock.ExpectQuery(`INSERT INTO provider_profiles`).
		WithArgs(
			"test-oauth2-provider",      // name
			"test-client-id",            // client_id
			"test-client-secret",        // client_secret
			"http://provider.com/auth",  // auth_url
			"http://provider.com/token", // token_url
			nil,                         // issuer
			false,                       // enable_discovery
			nil,                         // scopes
			"oauth2",                    // auth_type
			"",                          // auth_header (empty string)
			"",                          // api_base_url (empty string)
			"",                          // user_info_endpoint (empty string)
			sqlmock.AnyArg(),            // params
		).
		WillReturnRows(rows)

	profile := Profile{
		Name:            "test-oauth2-provider",
		AuthType:        "oauth2",
		ClientID:        "test-client-id",
		ClientSecret:    "test-client-secret",
		AuthURL:         "http://provider.com/auth",
		TokenURL:        "http://provider.com/token",
		EnableDiscovery: false,
		Params: func() *json.RawMessage {
			raw := json.RawMessage(`{"key":"value"}`)
			return &raw
		}(),
	}

	profileJSON, err := json.Marshal(profile)
	assert.NoError(t, err)

	result, err := store.RegisterProfile(string(profileJSON))
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, profile.Name, result.Name)
	assert.Equal(t, profile.AuthType, result.AuthType)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRegisterProfile_StaticKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name`).
		WithArgs("test-api-key-provider").
		WillReturnError(sql.ErrNoRows)

	rows := sqlmock.NewRows([]string{"id"}).AddRow("b1b1b1b1-b1b1-b1b1-b1b1-b1b1b1b1b1b1")
	mock.ExpectQuery(`INSERT INTO provider_profiles`).
		WithArgs(
			"test-api-key-provider", // name
			"",                      // client_id
			"",                      // client_secret
			"",                      // auth_url
			"",                      // token_url
			nil,                     // issuer
			false,                   // enable_discovery
			nil,                     // scopes (nil, not pq.Array)
			"api_key",               // auth_type
			"X-API-KEY",             // auth_header
			"",                      // api_base_url
			"",                      // user_info_endpoint
			sqlmock.AnyArg(),        // params
		).
		WillReturnRows(rows)

	profile := Profile{
		Name:       "test-api-key-provider",
		AuthType:   "api_key",
		AuthHeader: "X-API-KEY",
	}
	profileJSON, err := json.Marshal(profile)
	assert.NoError(t, err)

	_, err = store.RegisterProfile(string(profileJSON))
	assert.NoError(t, err)
}

func TestRegisterProfile_InvalidOAuth2(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	profile := Profile{
		Name:     "test-invalid-provider",
		AuthType: "oauth2",
	}
	profileJSON, err := json.Marshal(profile)
	assert.NoError(t, err)

	_, err = store.RegisterProfile(string(profileJSON))
	assert.Error(t, err)
	// Check for field-specific error from updated RegisterProfile
	assert.Contains(t, err.Error(), "client_id: missing required field")
}

func TestRegisterProfile_InvalidJSON(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	_, err = store.RegisterProfile("invalid json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

func TestRegisterProfile_NameCapitalLetters(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	profile := Profile{
		Name:         "TestWithCapital",
		AuthType:     "oauth2",
		ClientID:     "123",
		ClientSecret: "456",
		AuthURL:      "https://auth.com",
		TokenURL:     "https://token.com",
	}
	profileJSON, err := json.Marshal(profile)
	assert.NoError(t, err)

	_, err = store.RegisterProfile(string(profileJSON))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name: invalid provider name")
}
