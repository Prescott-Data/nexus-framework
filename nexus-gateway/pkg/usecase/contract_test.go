package usecase

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Gateway <-> Broker Contract Tests ---
// These tests verify that the Gateway correctly handles unexpected or
// changed Broker responses. If the Broker's API schema drifts, these
// tests will fail loudly, preventing silent regressions.

// TestBrokerContract_ConsentSpecMissingFields verifies that the Gateway
// gracefully handles a Broker response where expected fields are null or missing.
func TestBrokerContract_ConsentSpecMissingFields(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	t.Setenv("BROKER_API_KEY", "test-api-key")

	tests := []struct {
		name           string
		brokerResponse map[string]interface{}
		expectStatus   int
	}{
		{
			name: "Null auth_url — state is valid",
			brokerResponse: map[string]interface{}{
				"state":       generateState(key, "ws", "prov-1", "nonce-1"),
				"auth_url":    nil,
				"provider_id": "prov-1",
				"scopes":      []string{"email"},
			},
			expectStatus: http.StatusOK,
		},
		{
			name: "Missing state field entirely",
			brokerResponse: map[string]interface{}{
				"auth_url":    "https://example.com/auth",
				"provider_id": "prov-1",
			},
			expectStatus: http.StatusBadRequest, // Gateway returns 400 for invalid/missing state
		},
		{
			name:           "Empty broker response body (200 with no JSON)",
			brokerResponse: nil,
			expectStatus:   http.StatusBadGateway, // Gateway returns 502 when broker response can't be parsed
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Use a catch-all handler to avoid routing mismatches with the generated client
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if tc.brokerResponse == nil {
					w.WriteHeader(http.StatusOK)
					return
				}
				json.NewEncoder(w).Encode(tc.brokerResponse)
			}))
			defer server.Close()

			h := NewHandler(server.URL, key, nil)

			// Use provider_id directly to skip name resolution and isolate the consent-spec contract
			body := `{"user_id":"ws","provider_id":"prov-1","scopes":["email"],"return_url":"http://localhost"}`
			req := httptest.NewRequest("POST", "/v1/request-connection", strings.NewReader(body))
			w := httptest.NewRecorder()

			h.RequestConnection(w, req)

			if w.Code != tc.expectStatus {
				t.Errorf("expected status %d, got %d. Body: %s", tc.expectStatus, w.Code, w.Body.String())
			}
		})
	}
}

// TestBrokerContract_TokenEndpointSchemaChange verifies that the Gateway's
// CheckConnectionCore correctly interprets various Broker HTTP status codes.
// If the Broker changes its status code semantics, this test catches it.
func TestBrokerContract_TokenEndpointSchemaChange(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	t.Setenv("BROKER_API_KEY", "test-api-key")

	tests := []struct {
		name           string
		brokerStatus   int
		brokerBody     string
		expectedStatus string
	}{
		{
			name:           "200 OK maps to active",
			brokerStatus:   http.StatusOK,
			brokerBody:     `{"access_token":"tok","token_type":"Bearer"}`,
			expectedStatus: "active",
		},
		{
			name:           "404 Not Found maps to failed",
			brokerStatus:   http.StatusNotFound,
			brokerBody:     `{"error":"not_found"}`,
			expectedStatus: "failed",
		},
		{
			name:           "401 Unauthorized maps to failed",
			brokerStatus:   http.StatusUnauthorized,
			brokerBody:     `{"error":"unauthorized"}`,
			expectedStatus: "failed",
		},
		{
			name:           "202 Accepted (pending) maps to pending",
			brokerStatus:   http.StatusAccepted,
			brokerBody:     `{"status":"pending"}`,
			expectedStatus: "pending",
		},
		{
			name:           "500 Internal Server Error maps to pending (server-side issue)",
			brokerStatus:   http.StatusInternalServerError,
			brokerBody:     `{"error":"internal"}`,
			expectedStatus: "pending",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/connections/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.brokerStatus)
				w.Write([]byte(tc.brokerBody))
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			h := NewHandler(server.URL, key, nil)

			status, err := h.CheckConnectionCore(t.Context(), "conn-123")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if status != tc.expectedStatus {
				t.Errorf("expected status %q, got %q", tc.expectedStatus, status)
			}
		})
	}
}

// TestBrokerContract_BrokerDown verifies the Gateway returns a proper error
// when the Broker is completely unreachable.
func TestBrokerContract_BrokerDown(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	t.Setenv("BROKER_API_KEY", "test-api-key")

	// Point handler to a URL that won't respond
	h := NewHandler("http://127.0.0.1:1", key, nil)

	_, err := h.CheckConnectionCore(t.Context(), "conn-123")
	if err == nil {
		t.Fatal("expected error when broker is unreachable, got nil")
	}
}
