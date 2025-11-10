package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"dromos.com/oauth-broker/internal/auth"
	"dromos.com/oauth-broker/internal/vault"
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

func TestCaptureCredentialForm_ValidState(t *testing.T) {
	handler := NewCallbackHandler(nil, []byte("test-key"), []byte("test-key"), http.DefaultClient)

	stateData := auth.StateData{
		WorkspaceID: "ws-123",
		ProviderID:  "a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0a0",
		Nonce:       "nonce",
		IAT:         time.Now(),
	}
	signedState, err := auth.SignState([]byte("test-key"), stateData)
	assert.NoError(t, err)

	req, err := http.NewRequest("GET", "/auth/capture-credential?state="+signedState+"&provider_name=Test+Provider", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.CaptureCredentialForm(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "value=\""+signedState+"\"")
}

func TestSaveCredential_ValidState(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewCallbackHandler(sqlxDB, []byte("01234567890123456789012345678901"), []byte("01234567890123456789012345678901"), http.DefaultClient)

	stateData := auth.StateData{
		WorkspaceID: "ws-123",
		ProviderID:  "a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0a0",
		Nonce:       uuid.New().String(),
		IAT:         time.Now(),
	}
	signedState, err := auth.SignState([]byte("01234567890123456789012345678901"), stateData)
	assert.NoError(t, err)

	// Mock the query to get the return_url
	rows := sqlmock.NewRows([]string{"return_url"}).AddRow("http://localhost:3000/callback")
	mock.ExpectQuery("SELECT return_url FROM connections WHERE id = \\$1").
		WithArgs(uuid.MustParse(stateData.Nonce)).
		WillReturnRows(rows)

	mock.ExpectExec("INSERT INTO tokens \\(connection_id, encrypted_data, expires_at\\) VALUES \\(\\$1, \\$2, \\$3\\)").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("UPDATE connections SET status = \\$1, updated_at = NOW\\(\\) WHERE id = \\$2").
		WithArgs("active", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	form := url.Values{}
	form.Add("state", signedState)
	form.Add("credential", "test-credential")
	req, err := http.NewRequest("POST", "/auth/capture-credential", strings.NewReader(form.Encode()))
	assert.NoError(t, err)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	handler.SaveCredential(rr, req)

	assert.Equal(t, http.StatusFound, rr.Code)
	location := rr.Header().Get("Location")
	assert.True(t, strings.Contains(location, "http://localhost:3000/callback"))
}

func TestSaveCredential_InvalidState(t *testing.T) {
	handler := NewCallbackHandler(nil, []byte("test-key"), []byte("test-key"), http.DefaultClient)

	form := url.Values{}
	form.Add("state", "invalid-state")
	form.Add("credential", "test-credential")
	req, err := http.NewRequest("POST", "/auth/capture-credential", strings.NewReader(form.Encode()))
	assert.NoError(t, err)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	handler.SaveCredential(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}