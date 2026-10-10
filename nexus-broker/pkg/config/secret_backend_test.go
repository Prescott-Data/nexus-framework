package config

import (
	"strings"
	"testing"
)

func TestLoad_SecretBackend(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL":   "postgres://x",
		"BASE_URL":       "http://localhost",
		"ENCRYPTION_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"STATE_KEY":      "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	}
	cases := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"default is internal", nil, false},
		{"internal ignores a bad GCP prefix", map[string]string{"SECRET_BACKEND": "internal", "GCP_SECRET_PREFIX": "bad prefix!"}, false},
		{"gcp validates the prefix", map[string]string{"SECRET_BACKEND": "gcp-secret-manager", "GCP_SECRET_PREFIX": "bad prefix!"}, true},
		{"gcp rejects a prefix too long for a secret id", map[string]string{"SECRET_BACKEND": "gcp-secret-manager", "GCP_SECRET_PREFIX": strings.Repeat("p", 219)}, true},
		{"gcp with a good prefix", map[string]string{"SECRET_BACKEND": "gcp-secret-manager", "GCP_SECRET_PREFIX": "nexus-providers"}, false},
		{"unknown backend", map[string]string{"SECRET_BACKEND": "hashicorp-vault"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for k, v := range base {
				t.Setenv(k, v)
			}
			t.Setenv("SECRET_BACKEND", "")
			t.Setenv("GCP_SECRET_PREFIX", "")
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if c.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
