package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// stringPtr is a helper to get a pointer to a string
func stringPtr(s string) *string {
	return &s
}

func TestHealthWorker_CheckProvider_NonOAuth2_MissingURLs(t *testing.T) {
	worker := NewHealthWorker(nil, time.Minute)

	status, msg := worker.checkProvider(context.Background(), Profile{
		AuthType: "api_key",
	})
	assert.Equal(t, "unknown", status)
	assert.Contains(t, msg, "No API Base URL or User Info Endpoint configured")
}

func TestHealthWorker_CheckProvider_NonOAuth2_Healthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "HEAD", r.Method)
		w.WriteHeader(http.StatusUnauthorized) // 401 is healthy for a reachability check
	}))
	defer server.Close()

	worker := NewHealthWorker(nil, time.Minute)

	status, msg := worker.checkProvider(context.Background(), Profile{
		AuthType: "api_key",
		APIBaseURL: server.URL,
	})
	assert.Equal(t, "healthy", status)
	assert.Contains(t, msg, "Endpoint reachable (status 401)")
}

func TestHealthWorker_CheckProvider_NonOAuth2_Unhealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway) // 502
	}))
	defer server.Close()

	worker := NewHealthWorker(nil, time.Minute)

	status, msg := worker.checkProvider(context.Background(), Profile{
		AuthType: "api_key",
		UserInfoEndpoint: server.URL,
	})
	assert.Equal(t, "unhealthy", status)
	assert.Contains(t, msg, "Server Error (502)")
}

func TestHealthWorker_CheckProvider_MissingTokenURL(t *testing.T) {
	worker := NewHealthWorker(nil, time.Minute)

	status, msg := worker.checkProvider(context.Background(), Profile{
		AuthType: "oauth2",
		TokenURL: nil, // explicitly nil
	})
	assert.Equal(t, "unknown", status)
	assert.Contains(t, msg, "No token URL available")
}

func TestHealthWorker_CheckProvider_Healthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		
		err := r.ParseForm()
		assert.NoError(t, err)
		assert.Equal(t, "authorization_code", r.FormValue("grant_type"))
		assert.Equal(t, "dummy_code_for_health_check", r.FormValue("code"))
		
		w.WriteHeader(http.StatusBadRequest) // 400 is expected for dummy code
		_, _ = w.Write([]byte(`{"error": "invalid_grant"}`))
	}))
	defer server.Close()

	worker := NewHealthWorker(nil, time.Minute)

	status, msg := worker.checkProvider(context.Background(), Profile{
		AuthType: "oauth2",
		TokenURL: stringPtr(server.URL),
		ClientID: stringPtr("test-client"),
		ClientSecret: stringPtr("test-secret"),
	})

	assert.Equal(t, "healthy", status)
	assert.Contains(t, msg, "expected OAuth error")
}

func TestHealthWorker_CheckProvider_Unhealthy_500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	worker := NewHealthWorker(nil, time.Minute)

	status, msg := worker.checkProvider(context.Background(), Profile{
		AuthType: "oauth2",
		TokenURL: stringPtr(server.URL),
	})

	assert.Equal(t, "unhealthy", status)
	assert.Contains(t, msg, "Server Error (500)")
}

func TestHealthWorker_CheckProvider_Degraded_200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token": "wait_what"}`))
	}))
	defer server.Close()

	worker := NewHealthWorker(nil, time.Minute)

	status, msg := worker.checkProvider(context.Background(), Profile{
		AuthType: "oauth2",
		TokenURL: stringPtr(server.URL),
	})

	assert.Equal(t, "degraded", status)
	assert.Contains(t, msg, "Unexpected status code 200")
}

func TestHealthWorker_CheckProvider_NetworkError(t *testing.T) {
	worker := NewHealthWorker(nil, time.Minute)

	status, msg := worker.checkProvider(context.Background(), Profile{
		AuthType: "oauth2",
		TokenURL: stringPtr("http://localhost:12345/nonexistent-server-so-this-fails-to-connect"),
	})

	assert.Equal(t, "unhealthy", status)
	assert.Contains(t, msg, "Network error reaching token endpoint")
}
