package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

func TestGetSpec_OAuth2(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")

	// Use httptest.NewServer to create a real mock server for provider discovery
	mockProviderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// This is a simplified OIDC discovery response
		io.WriteString(w, `{"authorization_endpoint": "http://provider.com/auth"}`)
	}))
	defer mockProviderServer.Close()

	// Pass the test server's client to the handler
	handler := NewConsentHandler(sqlxDB, "http://localhost:8080", []byte("test-key"), mockProviderServer.Client())

	paramsJSON := []byte(`{"access_type": "offline", "prompt": "consent"}`)
	rows := sqlmock.NewRows([]string{"id", "name", "auth_type", "auth_url", "client_id", "scopes", "params"}).
		AddRow("a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0a0", "Test OAuth2 Provider", "oauth2", "http://provider.com/auth", "test-client-id", "{openid}", paramsJSON)
	mock.ExpectQuery("SELECT id, name, auth_type, auth_url, client_id, scopes, params FROM provider_profiles WHERE id = \\$1").
		WithArgs("a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0a0").
		WillReturnRows(rows)

	mock.ExpectExec("INSERT INTO connections").
		WillReturnResult(sqlmock.NewResult(1, 1))

	body := map[string]interface{}{
		"workspace_id": "ws-123",
		"provider_id":  "a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0a0",
		"scopes":       []string{"openid"},
		"return_url":   "http://localhost:3000/callback",
	}
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", "/auth/consent-spec", bytes.NewReader(jsonBody))
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.GetSpec(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response ConsentSpec
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)

	authURL, err := url.Parse(response.AuthURL)
	assert.NoError(t, err)
	q := authURL.Query()

	assert.True(t, strings.HasPrefix(response.AuthURL, "http://provider.com/auth"), "authUrl should start with the provider's auth_url")
	assert.NotEmpty(t, q.Get("code_challenge"), "authUrl should contain a code_challenge")
	assert.Equal(t, "offline", q.Get("access_type"))
	assert.Equal(t, "consent", q.Get("prompt"))
}

func TestGetSpec_StaticKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	// For static key tests, we can pass a default client as no external calls are made.
	handler := NewConsentHandler(sqlxDB, "http://localhost:8080", []byte("test-key"), http.DefaultClient)


rows := sqlmock.NewRows([]string{"id", "name", "auth_type", "auth_url", "client_id", "scopes", "params"}).
		AddRow("b1b1b1b1-b1b1-b1b1-b1b1-b1b1b1b1b1b1", "Test API", "api_key", "", "", "{}", []byte("{}"))
	mock.ExpectQuery("SELECT id, name, auth_type, auth_url, client_id, scopes, params FROM provider_profiles WHERE id = \\$1").
		WithArgs("b1b1b1b1-b1b1-b1b1-b1b1-b1b1b1b1b1b1").
		WillReturnRows(rows)

	mock.ExpectExec("INSERT INTO connections").
		WillReturnResult(sqlmock.NewResult(1, 1))

	body := map[string]interface{}{
		"workspace_id": "ws-123",
		"provider_id":  "b1b1b1b1-b1b1-b1b1-b1b1-b1b1b1b1b1b1",
		"scopes":       []string{"read"},
		"return_url":   "http://localhost:3000/callback",
	}
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", "/auth/consent-spec", bytes.NewReader(jsonBody))
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.GetSpec(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response ConsentSpec
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.True(t, strings.Contains(response.AuthURL, "/auth/capture-credential"), "authUrl should contain /auth/capture-credential")
	assert.True(t, strings.Contains(response.AuthURL, "state="), "authUrl should contain a state parameter")
}
