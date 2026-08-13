package provider

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

func ptr(s string) *string {
	return &s
}

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
			pq.Array([]string{}),        // scopes (empty array, not nil)
			"oauth2",                    // auth_type
			"",                          // auth_header (empty string)
			"",                          // api_base_url (empty string)
			"",                          // user_info_endpoint (empty string)
			sqlmock.AnyArg(),            // params
			"",                          // description
			"",                          // category
			nil,                         // saml_idp_entity_id
			nil,                         // saml_idp_sso_url
			nil,                         // saml_idp_x509_cert
			nil,                         // saml_sp_entity_id
		).
		WillReturnRows(rows)

	profile := Profile{
		Name:            "test-oauth2-provider",
		AuthType:        "oauth2",
		ClientID:        ptr("test-client-id"),
		ClientSecret:    ptr("test-client-secret"),
		AuthURL:         ptr("http://provider.com/auth"),
		TokenURL:        ptr("http://provider.com/token"),
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
			nil,                     // client_id
			nil,                     // client_secret
			nil,                     // auth_url
			nil,                     // token_url
			nil,                     // issuer
			false,                   // enable_discovery
			pq.Array([]string{}),    // scopes (empty array, not nil)
			"api_key",               // auth_type
			"X-API-KEY",             // auth_header
			"",                      // api_base_url
			"",                      // user_info_endpoint
			sqlmock.AnyArg(),        // params
			"",                      // description
			"",                      // category
			nil,                     // saml_idp_entity_id
			nil,                     // saml_idp_sso_url
			nil,                     // saml_idp_x509_cert
			nil,                     // saml_sp_entity_id
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
		ClientID:     ptr("123"),
		ClientSecret: ptr("456"),
		AuthURL:      ptr("https://auth.com"),
		TokenURL:     ptr("https://token.com"),
	}
	profileJSON, err := json.Marshal(profile)
	assert.NoError(t, err)

	_, err = store.RegisterProfile(string(profileJSON))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name: invalid provider name")
}

func TestGetProfile_NullValues(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	providerID := uuid.New()
	rows := sqlmock.NewRows([]string{
		"id", "name", "client_id", "client_secret", "auth_url", "token_url", "issuer",
		"enable_discovery", "scopes", "auth_type", "auth_header", "api_base_url", "user_info_endpoint", "params", "description", "category", "last_health_check_at", "health_status", "health_message", "saml_idp_entity_id", "saml_idp_sso_url", "saml_idp_x509_cert", "saml_sp_entity_id",
	}).AddRow(
		providerID.String(), "null-provider", nil, nil, nil, nil, nil,
		false, []byte("{}"), "api_key", "", "", "", nil, "", "", nil, "unknown", nil, nil, nil, nil, nil,
	)

	mock.ExpectQuery(`SELECT .* FROM provider_profiles WHERE id = \$1`).
		WithArgs(providerID).
		WillReturnRows(rows)

	profile, err := store.GetProfile(providerID)
	assert.NoError(t, err)
	assert.NotNil(t, profile)
	if profile != nil {
		assert.Equal(t, "null-provider", profile.Name)
	}
}

func TestGetAllProfiles_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	id1 := uuid.New()
	id2 := uuid.New()
	now := time.Now()
	msg := "timeout reaching token_endpoint"

	// Must match the exact 23-column order in GetAllProfiles SELECT:
	// id, name, client_id, client_secret, auth_url, token_url, issuer,
	// enable_discovery, scopes, auth_type, auth_header,
	// api_base_url, user_info_endpoint, params, description, category,
	// last_health_check_at, health_status, health_message, SAML metadata
	rows := sqlmock.NewRows([]string{
		"id", "name", "client_id", "client_secret", "auth_url", "token_url", "issuer",
		"enable_discovery", "scopes", "auth_type", "auth_header",
		"api_base_url", "user_info_endpoint", "params", "description", "category",
		"last_health_check_at", "health_status", "health_message", "saml_idp_entity_id", "saml_idp_sso_url", "saml_idp_x509_cert", "saml_sp_entity_id",
	}).AddRow(
		id1.String(), "google", ptr("cid"), ptr("csec"), ptr("https://auth"), ptr("https://token"), nil,
		true, []byte("{email,profile}"), "oauth2", "",
		"https://api.google.com", "/userinfo", nil, "Google OAuth", "Identity",
		now, "healthy", nil, nil, nil, nil, nil,
	).AddRow(
		id2.String(), "stripe", nil, nil, nil, nil, nil,
		false, []byte("{}"), "api_key", "Authorization",
		"https://api.stripe.com", "/v1/account", nil, "Stripe API", "Payments",
		now, "unhealthy", &msg, nil, nil, nil, nil,
	)

	mock.ExpectQuery(`SELECT .* FROM provider_profiles`).WillReturnRows(rows)

	profiles, err := store.GetAllProfiles()
	assert.NoError(t, err)
	assert.Len(t, profiles, 2)

	// Verify first profile health fields
	assert.Equal(t, id1, profiles[0].ID)
	assert.Equal(t, "google", profiles[0].Name)
	assert.Equal(t, "healthy", profiles[0].HealthStatus)
	assert.NotNil(t, profiles[0].LastHealthCheckAt)
	assert.Nil(t, profiles[0].HealthMessage)

	// Verify second profile health fields
	assert.Equal(t, id2, profiles[1].ID)
	assert.Equal(t, "stripe", profiles[1].Name)
	assert.Equal(t, "unhealthy", profiles[1].HealthStatus)
	assert.NotNil(t, profiles[1].HealthMessage)
	assert.Equal(t, "timeout reaching token_endpoint", *profiles[1].HealthMessage)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAllProfiles_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	rows := sqlmock.NewRows([]string{
		"id", "name", "client_id", "client_secret", "auth_url", "token_url", "issuer",
		"enable_discovery", "scopes", "auth_type", "auth_header",
		"api_base_url", "user_info_endpoint", "params", "description", "category",
		"last_health_check_at", "health_status", "health_message", "saml_idp_entity_id", "saml_idp_sso_url", "saml_idp_x509_cert", "saml_sp_entity_id",
	})

	mock.ExpectQuery(`SELECT .* FROM provider_profiles`).WillReturnRows(rows)

	profiles, err := store.GetAllProfiles()
	assert.NoError(t, err)
	assert.Nil(t, profiles) // append to nil slice returns nil

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAllProfiles_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	mock.ExpectQuery(`SELECT .* FROM provider_profiles`).
		WillReturnError(sql.ErrConnDone)

	profiles, err := store.GetAllProfiles()
	assert.Error(t, err)
	assert.Nil(t, profiles)
	assert.Contains(t, err.Error(), "failed to query all profiles")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateHealthStatus_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	providerID := uuid.New()
	msg := "token_url 503"

	// Verify the UPDATE is called with (status, message, id) in correct order
	mock.ExpectExec(`UPDATE provider_profiles SET health_status = \$1, health_message = \$2, last_health_check_at = NOW\(\) WHERE id = \$3`).
		WithArgs("unhealthy", &msg, providerID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = store.UpdateHealthStatus(providerID, "unhealthy", &msg)
	assert.NoError(t, err)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateHealthStatus_NilMessage(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	providerID := uuid.New()

	mock.ExpectExec(`UPDATE provider_profiles SET health_status = \$1, health_message = \$2, last_health_check_at = NOW\(\) WHERE id = \$3`).
		WithArgs("healthy", nil, providerID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = store.UpdateHealthStatus(providerID, "healthy", nil)
	assert.NoError(t, err)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateHealthStatus_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	providerID := uuid.New()

	mock.ExpectExec(`UPDATE provider_profiles SET health_status`).
		WithArgs("unhealthy", nil, providerID).
		WillReturnError(sql.ErrConnDone)

	err = store.UpdateHealthStatus(providerID, "unhealthy", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update provider health status")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetHealthStatus_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	providerID := uuid.New()
	rows := sqlmock.NewRows([]string{"health_status"}).AddRow("unhealthy")

	mock.ExpectQuery(`SELECT COALESCE\(health_status, 'unknown'\) FROM provider_profiles WHERE id = \$1`).
		WithArgs(providerID).
		WillReturnRows(rows)

	status, err := store.GetHealthStatus(providerID)
	assert.NoError(t, err)
	assert.Equal(t, "unhealthy", status)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetHealthStatus_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	providerID := uuid.New()

	mock.ExpectQuery(`SELECT COALESCE\(health_status, 'unknown'\) FROM provider_profiles WHERE id = \$1`).
		WithArgs(providerID).
		WillReturnError(sql.ErrNoRows)

	status, err := store.GetHealthStatus(providerID)
	assert.Error(t, err)
	assert.Equal(t, "", status)
	assert.Contains(t, err.Error(), "failed to get provider health status")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAllHealthStatuses_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	id1 := uuid.New()
	id2 := uuid.New()
	now := time.Now()
	msg := "503 from token_url"

	rows := sqlmock.NewRows([]string{"id", "name", "health_status", "last_health_check_at", "health_message"}).
		AddRow(id1.String(), "google", "healthy", now, nil).
		AddRow(id2.String(), "stripe", "unhealthy", now, &msg)

	mock.ExpectQuery(`SELECT id, name, COALESCE\(health_status, 'unknown'\), last_health_check_at, health_message FROM provider_profiles`).
		WillReturnRows(rows)

	summaries, err := store.GetAllHealthStatuses()
	assert.NoError(t, err)
	assert.Len(t, summaries, 2)

	assert.Equal(t, "google", summaries[0].Name)
	assert.Equal(t, "healthy", summaries[0].HealthStatus)
	assert.Nil(t, summaries[0].HealthMessage)

	assert.Equal(t, "stripe", summaries[1].Name)
	assert.Equal(t, "unhealthy", summaries[1].HealthStatus)
	assert.Equal(t, "503 from token_url", *summaries[1].HealthMessage)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAllHealthStatuses_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)

	rows := sqlmock.NewRows([]string{"id", "name", "health_status", "last_health_check_at", "health_message"})
	mock.ExpectQuery(`SELECT id, name, COALESCE\(health_status, 'unknown'\), last_health_check_at, health_message FROM provider_profiles`).
		WillReturnRows(rows)

	summaries, err := store.GetAllHealthStatuses()
	assert.NoError(t, err)
	assert.NotNil(t, summaries) // Should return [] not nil
	assert.Len(t, summaries, 0)

	assert.NoError(t, mock.ExpectationsWereMet())
}
func TestPatchProfile_SAMLRejectsMissingMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	store := NewStore(sqlx.NewDb(db, "sqlmock"))
	providerID := uuid.New()
	rows := sqlmock.NewRows(providerProfileColumns()).AddRow(
		providerID.String(), "oauth-provider", ptr("cid"), ptr("secret"), ptr("https://auth.example.com"), ptr("https://token.example.com"), nil,
		false, []byte("{}"), "oauth2", "", "", "", nil, "", "", nil, "unknown", nil, nil, nil, nil, nil,
	)

	mock.ExpectQuery(`SELECT .* FROM provider_profiles WHERE id = \$1`).
		WithArgs(providerID).
		WillReturnRows(rows)

	err = store.PatchProfile(providerID, map[string]interface{}{"auth_type": "saml"})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "saml_idp_entity_id")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPatchProfile_SAMLAcceptsCompleteMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	store := NewStore(sqlx.NewDb(db, "sqlmock"))
	providerID := uuid.New()
	cert := testSAMLProviderCertificatePEM(t)
	rows := sqlmock.NewRows(providerProfileColumns()).AddRow(
		providerID.String(), "placeholder-provider", nil, nil, nil, nil, nil,
		false, []byte("{}"), "api_key", "", "", "", nil, "", "", nil, "unknown", nil, nil, nil, nil, nil,
	)

	mock.ExpectQuery(`SELECT .* FROM provider_profiles WHERE id = \$1`).
		WithArgs(providerID).
		WillReturnRows(rows)
	mock.ExpectExec(`UPDATE provider_profiles SET`).
		WithArgs(
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			providerID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = store.PatchProfile(providerID, map[string]interface{}{
		"auth_type":          "saml",
		"saml_idp_entity_id": "https://idp.example.com/entity",
		"saml_idp_sso_url":   "https://idp.example.com/sso",
		"saml_idp_x509_cert": cert,
		"saml_sp_entity_id":  "https://broker.example.com/saml/sp",
	})

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func providerProfileColumns() []string {
	return []string{
		"id", "name", "client_id", "client_secret", "auth_url", "token_url", "issuer",
		"enable_discovery", "scopes", "auth_type", "auth_header", "api_base_url", "user_info_endpoint", "params", "description", "category", "last_health_check_at", "health_status", "health_message", "saml_idp_entity_id", "saml_idp_sso_url", "saml_idp_x509_cert", "saml_sp_entity_id",
	}
}
func TestRegisterProfile_SAML(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	store := NewStore(sqlxDB)
	cert := testSAMLProviderCertificatePEM(t)

	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name`).
		WithArgs("test-saml-provider").
		WillReturnError(sql.ErrNoRows)

	rows := sqlmock.NewRows([]string{"id"}).AddRow("c2c2c2c2-c2c2-c2c2-c2c2-c2c2c2c2c2c2")
	mock.ExpectQuery(`INSERT INTO provider_profiles`).
		WithArgs(
			"test-saml-provider",
			nil,
			nil,
			nil,
			nil,
			nil,
			false,
			pq.Array([]string{}),
			"saml",
			"",
			"",
			"",
			nil,
			"",
			"",
			"https://idp.example.com/entity",
			"https://idp.example.com/sso",
			cert,
			"https://broker.example.com/saml/sp",
		).
		WillReturnRows(rows)

	profile := Profile{
		Name:            "test-saml-provider",
		AuthType:        "saml",
		SAMLIdpEntityID: ptr("https://idp.example.com/entity"),
		SAMLIdpSSOURL:   ptr("https://idp.example.com/sso"),
		SAMLIdpX509Cert: ptr(cert),
		SAMLSPEntityID:  ptr("https://broker.example.com/saml/sp"),
	}
	profileJSON, err := json.Marshal(profile)
	assert.NoError(t, err)

	result, err := store.RegisterProfile(string(profileJSON))
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "saml", result.AuthType)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRegisterProfile_SAMLRejectsMissingMetadata(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	store := NewStore(sqlx.NewDb(db, "sqlmock"))
	profile := Profile{Name: "bad-saml-provider", AuthType: "saml"}
	profileJSON, err := json.Marshal(profile)
	assert.NoError(t, err)

	_, err = store.RegisterProfile(string(profileJSON))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "saml_idp_entity_id")
}

func testSAMLProviderCertificatePEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Test IdP"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
