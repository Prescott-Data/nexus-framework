package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApiKeyMiddleware(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	testCases := []struct {
		name           string
		require        bool
		apiKey         string
		headerKey      string
		expectedStatus int
	}{
		{
			name:           "Not required",
			require:        false,
			apiKey:         "",
			headerKey:      "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Valid key",
			require:        true,
			apiKey:         "valid-key",
			headerKey:      "valid-key",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Missing key",
			require:        true,
			apiKey:         "valid-key",
			headerKey:      "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Invalid key",
			require:        true,
			apiKey:         "valid-key",
			headerKey:      "invalid-key",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			keys := make(map[string]struct{})
			if tc.apiKey != "" {
				keys[tc.apiKey] = struct{}{}
			}

			req := httptest.NewRequest("GET", "/", nil)
			if tc.headerKey != "" {
				req.Header.Set("X-API-Key", tc.headerKey)
			}

			rr := httptest.NewRecorder()
			handler := ApiKeyMiddleware(tc.require, keys)(nextHandler)
			handler.ServeHTTP(rr, req)

			if status := rr.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", status, tc.expectedStatus)
			}
		})
	}
}

func TestApiKeySourceMiddlewareReloadsFileKeys(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "api-keys")
	if err := os.WriteFile(keyFile, []byte("first-key\n"), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	source, err := NewReloadingAPIKeySource(nil, []string{keyFile}, time.Second)
	if err != nil {
		t.Fatalf("NewReloadingAPIKeySource returned error: %v", err)
	}

	now := source.lastReload
	source.now = func() time.Time { return now }

	handler := ApiKeySourceMiddleware(true, source)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	if status := requestWithAPIKey(handler, "first-key"); status != http.StatusOK {
		t.Fatalf("expected first key to be accepted, got %d", status)
	}

	if err := os.WriteFile(keyFile, []byte("second-key\n"), 0600); err != nil {
		t.Fatalf("failed to rotate key file: %v", err)
	}

	if status := requestWithAPIKey(handler, "second-key"); status != http.StatusForbidden {
		t.Fatalf("expected second key to be rejected before reload interval, got %d", status)
	}

	now = now.Add(2 * time.Second)

	if status := requestWithAPIKey(handler, "second-key"); status != http.StatusOK {
		t.Fatalf("expected second key to be accepted after reload, got %d", status)
	}
	if status := requestWithAPIKey(handler, "first-key"); status != http.StatusForbidden {
		t.Fatalf("expected first key to be rejected after reload, got %d", status)
	}
}

func TestReloadingAPIKeySourceKeepsLastKeysWhenReloadFails(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "api-keys")
	if err := os.WriteFile(keyFile, []byte("stable-key\n"), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	source, err := NewReloadingAPIKeySource(nil, []string{keyFile}, time.Second)
	if err != nil {
		t.Fatalf("NewReloadingAPIKeySource returned error: %v", err)
	}

	now := source.lastReload
	source.now = func() time.Time { return now }

	if err := os.Remove(keyFile); err != nil {
		t.Fatalf("failed to remove key file: %v", err)
	}
	now = now.Add(2 * time.Second)

	if !source.Contains("stable-key") {
		t.Fatal("expected last known-good key to remain valid after reload failure")
	}
}
func TestNewReloadingAPIKeySourceFailsForMissingFile(t *testing.T) {
	missingFile := filepath.Join(t.TempDir(), "missing-api-keys")

	_, err := NewReloadingAPIKeySource(nil, []string{missingFile}, time.Second)
	if err == nil {
		t.Fatal("expected missing key file to fail initial load")
	}
}

func requestWithAPIKey(handler http.Handler, key string) int {
	req := httptest.NewRequest("GET", "/", nil)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr.Code
}
