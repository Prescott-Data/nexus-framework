package provider

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

func TestRegisterProfile_OAuth2(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	// 1. Mock duplicate provider check (no rows found)
	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name = \$1`).
		WithArgs("test-oauth2-provider").
		WillReturnError(sql.ErrNoRows)

	// 2. Mock INSERT query
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow("a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0a0")

	mock.ExpectQuery(`INSERT INTO provider_profiles`).
		WithArgs(
			"test-oauth2-provider",      // name
			"test-client-id",            // client_id
			"test-client-secret",        // client_secret
			"http://provider.com/auth",  // auth_url
			"http://provider.com/token", // token_url
			"",                          // issuer
			false,                       // enable_discovery
			pq.Array([]string(nil)),     // scopes
			"oauth2",                    // auth_type
			"",                          // auth_header
			"",                          // api_base_url
			"",                          // user_info_endpoint
			sqlmock.AnyArg(),            // params
		).
		WillReturnRows(rows)

	// 3. Valid OAuth2 profile
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

	// 4. Call RegisterProfile
	result, err := store.RegisterProfile(string(profileJSON))

	// 5. Assertions
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, profile.Name, result.Name)
	assert.Equal(t, profile.AuthType, result.AuthType)

	// 6. Ensure all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRegisterProfile_StaticKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	// 1. Mock the duplicate check query (should return no rows)
	mock.ExpectQuery("SELECT id FROM provider_profiles WHERE name").
		WithArgs("test-api-key-provider").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	// 2. Mock the db.QueryRow INSERT call.
	rows := sqlmock.NewRows([]string{"id"}).AddRow("b1b1b1b1-b1b1-b1b1-b1b1-b1b1b1b1b1b1")
	mock.ExpectQuery("INSERT INTO provider_profiles").
		WithArgs(
			"test-api-key-provider",
			"",
			"",
			"",
			"",
			nil,
			false,
			pq.Array([]string(nil)),
			"api_key",
			"X-API-KEY",
			"", // api_base_url
			"", // user_info_endpoint
			nil,
		).WillReturnRows(rows)

	// 2. Create a valid Profile struct with AuthType="api_key" and Name.
	profile := Profile{
		Name:       "test-api-key-provider",
		AuthType:   "api_key",
		AuthHeader: "X-API-KEY",
	}
	profileJSON, err := json.Marshal(profile)
	assert.NoError(t, err)

	// 3. Call store.RegisterProfile() with this profile.
	_, err = store.RegisterProfile(string(profileJSON))

	// 4. Assert that the err is nil.
	assert.NoError(t, err)
}

func TestRegisterProfile_InvalidOAuth2(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	// 1. Create an invalid Profile struct with AuthType="oauth2" but missing ClientID.
	profile := Profile{
		Name:     "test-invalid-provider",
		AuthType: "oauth2",
	}
	profileJSON, err := json.Marshal(profile)
	assert.NoError(t, err)

	// 2. Call store.RegisterProfile() with this invalid profile.
	_, err = store.RegisterProfile(string(profileJSON))

	// 3. Assert that an err is returned and that the error message indicates missing fields.
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing required oauth2 fields (name, client_id, client_secret, auth_url, token_url)")
}

func TestRegisterProfile_InvalidJSON(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	// Call store.RegisterProfile() with invalid JSON.
	_, err = store.RegisterProfile("invalid json")

	// Assert that a JSON decoding error is returned.
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}
