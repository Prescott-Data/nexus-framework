package config

import "testing"

func TestBackendAuthURL(t *testing.T) {
	for _, value := range []string{"", "https://auth.example.com", "http://127.0.0.1:8089", "http://auth.example.com", "ftp://host", "https://user:pass@host", "https://host?token=x", "not-a-url"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://localhost/db")
			t.Setenv("BASE_URL", "http://localhost")
			t.Setenv("ENCRYPTION_KEY", testKey())
			t.Setenv("STATE_KEY", testKey())
			t.Setenv("BACKEND_AUTH_URL", value)
			_, err := Load()
			valid := value == "" || value == "https://auth.example.com" || value == "http://127.0.0.1:8089"
			if valid && err != nil {
				t.Fatal(err)
			}
			if !valid && err == nil {
				t.Fatal("expected invalid backend URL to fail startup")
			}
		})
	}
}
