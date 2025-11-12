package server

import (
	"testing"
)

func TestIsReturnURLAllowed(t *testing.T) {
	testCases := []struct {
		name          string
		enforce       string
		allowed       string
		url           string
		expected      bool
	}{
		{
			name:     "Enforcement disabled",
			enforce:  "false",
			allowed:  "",
			url:      "https://any.com",
			expected: true,
		},
		{
			name:     "Simple match",
			enforce:  "true",
			allowed:  "example.com",
			url:      "https://example.com/foo",
			expected: true,
		},
		{
			name:     "Simple mismatch",
			enforce:  "true",
			allowed:  "example.com",
			url:      "https://bad.com/foo",
			expected: false,
		},
		{
			name:     "URL with port",
			enforce:  "true",
			allowed:  "localhost",
			url:      "http://localhost:3000",
			expected: true,
		},
		{
			name:     "Wildcard match",
			enforce:  "true",
			allowed:  "*.example.com",
			url:      "https://app.example.com",
			expected: true,
		},
		{
			name:     "Invalid URL",
			enforce:  "true",
			allowed:  "example.com",
			url:      "://invalid-url",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENFORCE_RETURN_URL", tc.enforce)
			t.Setenv("ALLOWED_RETURN_DOMAINS", tc.allowed)

			if got := IsReturnURLAllowed(tc.url); got != tc.expected {
				t.Errorf("IsReturnURLAllowed() = %v, want %v", got, tc.expected)
			}
		})
	}
}
