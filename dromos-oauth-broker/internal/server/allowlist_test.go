package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllowlistMiddleware(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	testCases := []struct {
		name           string
		require        string
		cidrs          string
		remoteAddr     string
		forwardedFor   string
		expectedStatus int
	}{
		{
			name:           "Not required",
			require:        "false",
			cidrs:          "",
			remoteAddr:     "1.1.1.1:12345",
			forwardedFor:   "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Allowed from RemoteAddr",
			require:        "true",
			cidrs:          "192.168.1.0/24",
			remoteAddr:     "192.168.1.10:12345",
			forwardedFor:   "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Disallowed from RemoteAddr",
			require:        "true",
			cidrs:          "192.168.1.0/24",
			remoteAddr:     "10.0.0.5:12345",
			forwardedFor:   "",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "Allowed from X-Forwarded-For",
			require:        "true",
			cidrs:          "10.0.0.0/16",
			remoteAddr:     "1.1.1.1:12345",
			forwardedFor:   "10.0.5.1",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Disallowed from X-Forwarded-For",
			require:        "true",
			cidrs:          "10.0.0.0/16",
			remoteAddr:     "1.1.1.1:12345",
			forwardedFor:   "172.16.0.1",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REQUIRE_ALLOWLIST", tc.require)
			t.Setenv("ALLOWED_CIDRS", tc.cidrs)

			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.forwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tc.forwardedFor)
			}

			rr := httptest.NewRecorder()
			handler := AllowlistMiddleware()(nextHandler)
			handler.ServeHTTP(rr, req)

			if status := rr.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", status, tc.expectedStatus)
			}
		})
	}
}
