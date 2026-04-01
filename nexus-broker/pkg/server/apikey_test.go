package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApiKeyMiddleware(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	testCases := []struct {
		name           string
		require        string
		apiKey         string
		headerKey      string
		expectedStatus int
	}{
		{
			name:           "Not required",
			require:        "false",
			apiKey:         "",
			headerKey:      "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Valid key",
			require:        "true",
			apiKey:         "valid-key",
			headerKey:      "valid-key",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Missing key",
			require:        "true",
			apiKey:         "valid-key",
			headerKey:      "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Invalid key",
			require:        "true",
			apiKey:         "valid-key",
			headerKey:      "invalid-key",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REQUIRE_API_KEY", tc.require)
			t.Setenv("API_KEY", tc.apiKey)

			req := httptest.NewRequest("GET", "/", nil)
			if tc.headerKey != "" {
				req.Header.Set("X-API-Key", tc.headerKey)
			}

			rr := httptest.NewRecorder()
			handler := ApiKeyMiddleware()(nextHandler)
			handler.ServeHTTP(rr, req)

			if status := rr.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", status, tc.expectedStatus)
			}
		})
	}
}
