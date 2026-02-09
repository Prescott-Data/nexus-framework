package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"dromos.com/nexus-broker/internal/auth"
	"dromos.com/nexus-broker/internal/vault"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

func TestRefresh_StaticKeyProvider(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewCallbackHandler(sqlxDB, []byte("test-key"), []byte("test-key"), http.DefaultClient)

	// Mock the initial query to find the connection

	rows := sqlmock.NewRows([]string{"provider_id", "auth_type"}).
		AddRow(uuid.New().String(), "api_key") // Use a new UUID for provider_id

	mock.ExpectQuery("SELECT c.provider_id, p.auth_type FROM connections c JOIN provider_profiles p ON c.provider_id = p.id WHERE c.id=\\$1 AND c.status='active'").
		WithArgs(uuid.MustParse("b1b1b1b1-b1b1-b1b1-b1b1-b1b1b1b1b1b1")). // Match the connection ID from the request
		WillReturnRows(rows)

	req, err := http.NewRequest("POST", "/connections/b1b1b1b1-b1b1-b1b1-b1b1-b1b1b1b1b1b1/refresh", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.Refresh(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "This connection uses a static token and cannot be refreshed")
}

func TestRefresh_OAuth2Provider(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")

	// Mock the external HTTP call to the provider's token URL
	mockProviderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token": "new-access-token", "refresh_token": "new-refresh-token", "expires_in": 3600}`)
	}))
	defer mockProviderServer.Close()

	handler := NewCallbackHandler(sqlxDB, []byte("01234567890123456789012345678901"), []byte("01234567890123456789012345678901"), mockProviderServer.Client())

	// Mock the initial query to find the connection

	rows := sqlmock.NewRows([]string{"provider_id", "auth_type"}).
		AddRow(uuid.New().String(), "oauth2")
	mock.ExpectQuery("SELECT c.provider_id, p.auth_type FROM connections c JOIN provider_profiles p ON c.provider_id = p.id WHERE c.id=\\$1 AND c.status='active'").
		WithArgs(uuid.MustParse("b1b1b1b1-b1b1-b1b1-b1b1-b1b1b1b1b1b1")).
		WillReturnRows(rows)

	mock.ExpectQuery("SELECT token_url, client_id, client_secret FROM provider_profiles WHERE id=\\$1").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"token_url", "client_id", "client_secret"}).
			AddRow(mockProviderServer.URL, "test-client-id", "test-client-secret"))

		// Encrypt the token before mocking the query

	tokenData := map[string]interface{}{"refresh_token": "test-refresh-token"}
	tokenJSON, _ := json.Marshal(tokenData)
	encryptedToken, err := vault.Encrypt([]byte("01234567890123456789012345678901"), tokenJSON)
	assert.NoError(t, err)

	mock.ExpectQuery("SELECT encrypted_data FROM tokens WHERE connection_id=\\$1 ORDER BY created_at DESC LIMIT 1").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"encrypted_data"}).AddRow(encryptedToken))

	mock.ExpectExec("INSERT INTO tokens").
		WillReturnResult(sqlmock.NewResult(1, 1))

	req, err := http.NewRequest("POST", "/connections/b1b1b1b1-b1b1-b1b1-b1b1-b1b1b1b1b1b1/refresh", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.Refresh(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetCaptureSchema(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	// Use a real key for signing/verifying state
	stateKey := []byte("01234567890123456789012345678901")
	handler := NewCallbackHandler(sqlxDB, nil, stateKey, http.DefaultClient)

	providerID := uuid.New()
	stateData := auth.StateData{
		ProviderID: providerID.String(),
		Nonce:      "test-nonce",
		IAT:        time.Now(),
	}
	signedState, err := auth.SignState(stateKey, stateData)
	assert.NoError(t, err)

	// Mock the database query
	mockSchema := `{"type":"object","properties":{"api_key":{"type":"string"}}}`
	mockParams := json.RawMessage(`{"credential_schema":` + mockSchema + `}`)
	mockParamsBytes, _ := json.Marshal(mockParams)

	rows := sqlmock.NewRows([]string{"name", "params"}).
		AddRow("Test Provider", mockParamsBytes)

	mock.ExpectQuery("SELECT name, params FROM provider_profiles WHERE id = \\$1").
		WithArgs(providerID).
		WillReturnRows(rows)

	// Create the request
	req, err := http.NewRequest("GET", "/auth/capture-schema?state="+url.QueryEscape(signedState), nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.GetCaptureSchema(rr, req)

	// Assert the response
	assert.Equal(t, http.StatusOK, rr.Code)

	// Assert the body
	var respBody struct {
		ProviderName string          `json:"provider_name"`
		Schema       json.RawMessage `json:"schema"`
	}
	err = json.Unmarshal(rr.Body.Bytes(), &respBody)
	assert.NoError(t, err)

	assert.Equal(t, "Test Provider", respBody.ProviderName)
	assert.JSONEq(t, mockSchema, string(respBody.Schema))
}

func TestSaveCredential_ValidState(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	stateKey := []byte("01234567890123456789012345678901")
	encryptionKey := []byte("01234567890123456789012345678901")
	handler := NewCallbackHandler(sqlxDB, encryptionKey, stateKey, http.DefaultClient)

	connectionID := uuid.New()
	stateData := auth.StateData{
		Nonce: connectionID.String(),
		IAT:   time.Now(),
	}
	signedState, err := auth.SignState(stateKey, stateData)
	assert.NoError(t, err)

	// Mock DB calls
	mock.ExpectQuery("SELECT return_url FROM connections WHERE id = \\$1").
		WithArgs(connectionID).
		WillReturnRows(sqlmock.NewRows([]string{"return_url"}).AddRow("http://localhost:3000/callback"))

	// 1. Mock the call to storeTokens
	mock.ExpectExec(
		"INSERT INTO tokens \\(connection_id, encrypted_data, expires_at\\) VALUES \\(\\$1, \\$2, \\$3\\)",
	).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))

	// 2. Mock the call to updateConnectionStatus
	mock.ExpectExec(
		"UPDATE connections SET status = \\$1, updated_at = NOW\\(\\) WHERE id = \\$2",
	).WithArgs("active", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))

	// Create request body
	creds := map[string]interface{}{"api_key": "test-key"}
	body := map[string]interface{}{
		"state":       signedState,
		"credentials": creds,
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", "/auth/capture-credential", bytes.NewBuffer(jsonBody))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.SaveCredential(rr, req)

	assert.Equal(t, http.StatusFound, rr.Code)
	location := rr.Header().Get("Location")
	assert.Contains(t, location, "http://localhost:3000/callback")
	assert.Contains(t, location, "status=success")
	assert.Contains(t, location, "connection_id="+connectionID.String())
}

func TestSaveCredential_InvalidState(t *testing.T) {
	handler := NewCallbackHandler(nil, nil, []byte("test-key"), http.DefaultClient)

	creds := map[string]interface{}{"api_key": "test-key"}
	body := map[string]interface{}{
		"state":       "invalid-state",
		"credentials": creds,
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", "/auth/capture-credential", bytes.NewBuffer(jsonBody))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.SaveCredential(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "Invalid state")
}

func TestSaveCredential_InvalidJSON(t *testing.T) {
	handler := NewCallbackHandler(nil, nil, nil, http.DefaultClient)

	req, err := http.NewRequest("POST", "/auth/capture-credential", bytes.NewBuffer([]byte("not-json")))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.SaveCredential(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "Invalid JSON body")
}
