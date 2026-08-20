package config

import (
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"
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

func TestLoad_APIKeyFilesAndReloadInterval(t *testing.T) {
	apiKeyFile := filepath.Join(t.TempDir(), "api-key")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("BASE_URL", "http://localhost:8080")
	t.Setenv("ENCRYPTION_KEY", testKey())
	t.Setenv("STATE_KEY", testKey())
	t.Setenv("API_KEY_FILE", apiKeyFile)
	t.Setenv("API_KEYS_FILE", " /var/run/secrets/api-keys ")
	t.Setenv("API_KEY_RELOAD_INTERVAL", "5s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIKeyReloadInterval != 5*time.Second {
		t.Errorf("expected reload interval 5s, got %v", cfg.APIKeyReloadInterval)
	}
	if len(cfg.APIKeyFiles) != 2 {
		t.Fatalf("expected 2 api key files, got %d", len(cfg.APIKeyFiles))
	}
	if cfg.APIKeyFiles[0] != apiKeyFile {
		t.Errorf("expected first api key file %q, got %q", apiKeyFile, cfg.APIKeyFiles[0])
	}
	if cfg.APIKeyFiles[1] != "/var/run/secrets/api-keys" {
		t.Errorf("expected trimmed second api key file, got %q", cfg.APIKeyFiles[1])
	}
}

func TestLoad_InvalidAPIKeyReloadInterval(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("BASE_URL", "http://localhost:8080")
	t.Setenv("ENCRYPTION_KEY", testKey())
	t.Setenv("STATE_KEY", testKey())
	t.Setenv("API_KEY_RELOAD_INTERVAL", "0s")

	_, err := Load()
	if err == nil {
		t.Fatal("expected invalid reload interval error")
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
