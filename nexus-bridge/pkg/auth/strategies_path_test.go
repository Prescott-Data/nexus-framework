package auth

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyAuthentication_PathTemplate(t *testing.T) {
	// Telegram-style: the credential is embedded in the URL path.
	req, err := http.NewRequest("GET", "https://api.telegram.org/bot{api_key}/getMe", nil)
	assert.NoError(t, err)

	err = ApplyAuthentication(req, AuthStrategy{Type: "path"}, Credentials{"api_key": "123:ABC"})
	assert.NoError(t, err)
	assert.Equal(t, "/bot123:ABC/getMe", req.URL.Path)
	// No auth header/query should be added for path strategy.
	assert.Empty(t, req.Header.Get("Authorization"))
}

func TestApplyAuthentication_PathTemplateWithHeaderStrategy(t *testing.T) {
	// Path placeholders are rendered even when a header strategy is in effect.
	req, err := http.NewRequest("GET", "https://api.example.com/v1/{account_id}/me", nil)
	assert.NoError(t, err)

	err = ApplyAuthentication(req, AuthStrategy{
		Type:   "header",
		Config: map[string]interface{}{"header_name": "X-API-Key", "credential_field": "api_key"},
	}, Credentials{"api_key": "secret", "account_id": "acct_9"})
	assert.NoError(t, err)
	assert.Equal(t, "/v1/acct_9/me", req.URL.Path)
	assert.Equal(t, "secret", req.Header.Get("X-API-Key"))
}

func TestApplyAuthentication_UnknownPlaceholderUntouched(t *testing.T) {
	req, err := http.NewRequest("GET", "https://api.example.com/{missing}/x", nil)
	assert.NoError(t, err)

	err = ApplyAuthentication(req, AuthStrategy{Type: "path"}, Credentials{})
	assert.NoError(t, err)
	assert.Equal(t, "/{missing}/x", req.URL.Path)
}
