package config

import (
	"encoding/base64"
	"testing"
)

func testKey() string {
	return base64.StdEncoding.EncodeToString(make([]byte, 32))
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("BASE_URL", "http://localhost")
	t.Setenv("ENCRYPTION_KEY", testKey())
	t.Setenv("STATE_KEY", testKey())

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing DATABASE_URL")
	}
}

func TestLoad_MissingBaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("BASE_URL", "")
	t.Setenv("ENCRYPTION_KEY", testKey())
	t.Setenv("STATE_KEY", testKey())

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing BASE_URL")
	}
}

func TestLoad_MissingEncryptionKey(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("BASE_URL", "http://localhost")
	t.Setenv("ENCRYPTION_KEY", "")
	t.Setenv("STATE_KEY", testKey())

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing ENCRYPTION_KEY")
	}
}

func TestLoad_Success(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("BASE_URL", "http://localhost:8080")
	t.Setenv("ENCRYPTION_KEY", testKey())
	t.Setenv("STATE_KEY", testKey())
	t.Setenv("REQUIRE_API_KEY", "true")
	t.Setenv("API_KEYS", "key1,key2")
	t.Setenv("ALLOWED_RETURN_DOMAINS", "example.com,*.foo.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("expected default port 8080, got %s", cfg.Port)
	}
	if !cfg.RequireAPIKey {
		t.Error("expected RequireAPIKey true")
	}
	if _, ok := cfg.APIKeys["key1"]; !ok {
		t.Error("expected key1 in APIKeys")
	}
	if _, ok := cfg.APIKeys["key2"]; !ok {
		t.Error("expected key2 in APIKeys")
	}
	if len(cfg.AllowedReturnDomains) != 2 {
		t.Errorf("expected 2 allowed domains, got %d", len(cfg.AllowedReturnDomains))
	}
	if cfg.RedirectPath != "/auth/callback" {
		t.Errorf("expected default redirect path, got %s", cfg.RedirectPath)
	}
}

func TestLoad_DBSSLEnforcement(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("BASE_URL", "http://localhost")
	t.Setenv("ENCRYPTION_KEY", testKey())
	t.Setenv("STATE_KEY", testKey())
	t.Setenv("ENFORCE_DB_SSL", "true")
	t.Setenv("DB_SSLMODE", "verify-full")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseURL == "postgres://localhost/db" {
		t.Error("expected DatabaseURL to have sslmode appended")
	}
}
