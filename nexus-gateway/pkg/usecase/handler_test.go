package usecase

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-gateway/pkg/broker"
)

// generateState creates a valid signed state string for testing
func generateState(key []byte, wsID, provID, nonce string) string {
	data := map[string]interface{}{
		"workspace_id": wsID,
		"provider_id":  provID,
		"nonce":        nonce,
		"iat":          time.Now(),
	}
	dataBytes, _ := json.Marshal(data)

	mac := hmac.New(sha256.New, key)
	mac.Write(dataBytes)
	sig := mac.Sum(nil)

	return base64.RawURLEncoding.EncodeToString(dataBytes) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func ptr[T any](v T) *T { return &v }

// mockBrokerServer creates a test HTTP server that mocks Broker endpoints
func mockBrokerServer(t *testing.T, key []byte) *httptest.Server {
	mux := http.NewServeMux()

	// Mock POST /auth/consent-spec
	mux.HandleFunc("/auth/consent-spec", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Validate headers
		if r.Header.Get("X-API-Key") != "test-api-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req broker.ConsentSpecRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Generate valid state
		state := generateState(key, req.WorkspaceId, *req.ProviderId, "test-nonce")

		// Return success response
		resp := broker.ConsentSpecResponse{
			AuthUrl:    ptr("https://mock-provider.com/auth?state=xyz"),
			State:      ptr(state),
			ProviderId: req.ProviderId,
			Scopes:     req.Scopes,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Mock GET /providers/metadata
	mux.HandleFunc("/providers/metadata", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Construct response using generic map to avoid strict typing issues with generated anonymous structs
		resp := map[string]map[string]interface{}{
			"oauth2": {
				"google": map[string]interface{}{
					"api_base_url": "https://api.google.com",
					"scopes":       []string{"email"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Mock GET /providers (list) - needed for resolveProviderID fallback
	mux.HandleFunc("/providers", func(w http.ResponseWriter, r *http.Request) {
		resp := []map[string]interface{}{
			{"id": "google-uuid", "name": "google"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Mock GET /providers/by-name/google
	mux.HandleFunc("/providers/by-name/google", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{"id": "google-uuid"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	return httptest.NewServer(mux)
}

// TestGetProviders verifies the metadata proxy
func TestGetProviders(t *testing.T) {
	server := mockBrokerServer(t, []byte("dummy"))
	defer server.Close()

	// Setup handler pointing to mock server
	t.Setenv("BROKER_API_KEY", "test-api-key")
	h := NewHandler(server.URL, []byte("test-secret-key"), nil)

	// Create request
	req := httptest.NewRequest("GET", "/v1/providers", nil)
	w := httptest.NewRecorder()

	// Execute
	h.GetProviders(w, req)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	oauth2, ok := resp["oauth2"]
	if !ok {
		t.Fatal("expected oauth2 key")
	}
	google, ok := oauth2["google"].(map[string]interface{})
	if !ok {
		t.Fatal("expected google key")
	}

	if google["api_base_url"] != "https://api.google.com" {
		t.Errorf("unexpected response content: %v", resp)
	}
}

// TestRequestConnection verifies connection initiation flow
func TestRequestConnection(t *testing.T) {
	key := []byte("12345678901234567890123456789012") // 32 bytes
	server := mockBrokerServer(t, key)
	defer server.Close()

	t.Setenv("BROKER_API_KEY", "test-api-key")
	h := NewHandler(server.URL, key, nil)

	// Request body
	body := map[string]interface{}{
		"user_id":     "test-ws",
		"provider_id": "test-provider",
		"scopes":      []string{"email"},
		"return_url":  "http://localhost",
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/request-connection", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()

	h.RequestConnection(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp["connection_id"] != "test-nonce" {
		t.Errorf("expected connection_id 'test-nonce', got '%v'", resp["connection_id"])
	}
}

func TestCreateProvider_SAMLPassThrough(t *testing.T) {
	brokerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/providers" || r.Method != http.MethodPost {
			t.Fatalf("unexpected broker request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "test-api-key" {
			t.Fatalf("missing broker API key")
		}

		var body broker.PostProvidersJSONRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode broker body: %v", err)
		}
		if body.Profile == nil {
			t.Fatalf("missing provider profile")
		}
		if body.Profile.AuthType == nil || *body.Profile.AuthType != broker.ProviderProfileAuthTypeSaml {
			t.Fatalf("auth_type was not preserved as saml: %#v", body.Profile.AuthType)
		}
		if body.Profile.SamlIdpEntityId == nil || *body.Profile.SamlIdpEntityId != "https://idp.example.com/entity" {
			t.Fatalf("saml_idp_entity_id was not preserved")
		}
		if body.Profile.SamlIdpSsoUrl == nil || *body.Profile.SamlIdpSsoUrl != "https://idp.example.com/sso" {
			t.Fatalf("saml_idp_sso_url was not preserved")
		}
		if body.Profile.SamlIdpX509Cert == nil || *body.Profile.SamlIdpX509Cert != "cert-pem" {
			t.Fatalf("saml_idp_x509_cert was not preserved")
		}
		if body.Profile.SamlSpEntityId == nil || *body.Profile.SamlSpEntityId != "https://broker.example.com/saml/sp" {
			t.Fatalf("saml_sp_entity_id was not preserved")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "provider-id", "message": "created"})
	}))
	defer brokerServer.Close()

	t.Setenv("BROKER_API_KEY", "test-api-key")
	h := NewHandler(brokerServer.URL, []byte("test-secret-key"), nil)

	payload := map[string]interface{}{
		"profile": map[string]interface{}{
			"name":               "okta-saml",
			"auth_type":          "saml",
			"saml_idp_entity_id": "https://idp.example.com/entity",
			"saml_idp_sso_url":   "https://idp.example.com/sso",
			"saml_idp_x509_cert": "cert-pem",
			"saml_sp_entity_id":  "https://broker.example.com/saml/sp",
		},
	}
	jsonBody, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/providers", bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()

	h.CreateProvider(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d. Body: %s", w.Code, w.Body.String())
	}
}
