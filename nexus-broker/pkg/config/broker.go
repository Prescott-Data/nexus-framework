package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// BrokerConfig holds all configuration for the nexus-broker service.
// Populated once at startup from environment variables, then passed to all
// subsystems. No other package should read os.Getenv directly.
type BrokerConfig struct {
	Port        string
	DatabaseURL string
	BaseURL     string
	RedisURL    string

	EncryptionKey []byte
	StateKey      []byte

	RedirectPath string

	// API key protection
	RequireAPIKey        bool
	APIKeys              map[string]struct{}
	APIKeyFiles          []string
	APIKeyReloadInterval time.Duration

	// CIDR allowlist
	RequireAllowlist bool
	AllowedCIDRs     string

	// Return URL enforcement
	EnforceReturnURL     bool
	AllowedReturnDomains []string

	// DB SSL enforcement
	EnforceDBSSL  bool
	DBSSLMode     string
	DBSSLRootCert string

	// Schema migrations applied on boot. On by default: the binary knows which
	// schema it needs, and a deployment that has to supply that separately can
	// disagree with it. Set AUTO_MIGRATE=false where migrations are run by a
	// separate step, such as a Kubernetes job.
	AutoMigrate   bool
	MigrationsDir string
}

// Load reads all configuration from environment variables, validates required
// fields, and returns a fully populated BrokerConfig or a fatal error.
func Load() (*BrokerConfig, error) {
	cfg := &BrokerConfig{
		Port:        envOr("PORT", "8080"),
		DatabaseURL: envOr("DATABASE_URL", ""),
		BaseURL:     envOr("BASE_URL", ""),
		RedisURL:    envOr("REDIS_URL", "redis://localhost:6379/0"),

		RedirectPath: envOr("REDIRECT_PATH", "/auth/callback"),

		RequireAPIKey:        envBool("REQUIRE_API_KEY"),
		RequireAllowlist:     envBool("REQUIRE_ALLOWLIST"),
		AllowedCIDRs:         envOr("ALLOWED_CIDRS", "127.0.0.1/32,::1/128"),
		APIKeyReloadInterval: 30 * time.Second,

		EnforceReturnURL: envBool("ENFORCE_RETURN_URL"),

		EnforceDBSSL:  envBool("ENFORCE_DB_SSL"),
		DBSSLMode:     envOr("DB_SSLMODE", "require"),
		DBSSLRootCert: strings.TrimSpace(os.Getenv("DB_SSLROOTCERT")),

		AutoMigrate:   envBoolDefault("AUTO_MIGRATE", true),
		MigrationsDir: envOr("MIGRATIONS_DIR", "/app/migrations"),
	}

	// Parse allowed return domains
	if raw := strings.TrimSpace(os.Getenv("ALLOWED_RETURN_DOMAINS")); raw != "" {
		for _, d := range strings.Split(raw, ",") {
			d = strings.ToLower(strings.TrimSpace(d))
			if d != "" {
				cfg.AllowedReturnDomains = append(cfg.AllowedReturnDomains, d)
			}
		}
	}

	// Build API key allow-set
	cfg.APIKeys = make(map[string]struct{})
	if v := strings.TrimSpace(os.Getenv("API_KEYS")); v != "" {
		for _, k := range strings.Split(v, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				cfg.APIKeys[k] = struct{}{}
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv("API_KEY")); v != "" {
		cfg.APIKeys[v] = struct{}{}
	}
	for _, envName := range []string{"API_KEY_FILE", "API_KEYS_FILE"} {
		if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
			for _, path := range strings.Split(v, ",") {
				path = strings.TrimSpace(path)
				if path != "" {
					cfg.APIKeyFiles = append(cfg.APIKeyFiles, path)
				}
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv("API_KEY_RELOAD_INTERVAL")); v != "" {
		interval, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("API_KEY_RELOAD_INTERVAL is invalid: %w", err)
		}
		if interval <= 0 {
			return nil, fmt.Errorf("API_KEY_RELOAD_INTERVAL must be greater than zero")
		}
		cfg.APIKeyReloadInterval = interval
	}

	// Required fields
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required")
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("BASE_URL environment variable is required")
	}

	// Cryptographic keys
	var err error
	cfg.EncryptionKey, err = ValidateKey("ENCRYPTION_KEY", os.Getenv("ENCRYPTION_KEY"))
	if err != nil {
		return nil, err
	}
	cfg.StateKey, err = ValidateKey("STATE_KEY", os.Getenv("STATE_KEY"))
	if err != nil {
		return nil, err
	}

	// Enforce DB SSL if configured
	cfg.DatabaseURL = enforceDBSSL(cfg.DatabaseURL, cfg.EnforceDBSSL, cfg.DBSSLMode, cfg.DBSSLRootCert)

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string) bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(key)), "true")
}

// envBoolDefault reads a boolean that is on unless explicitly turned off.
func envBoolDefault(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	return strings.EqualFold(raw, "true") || raw == "1"
}

func enforceDBSSL(dsn string, enforce bool, mode, rootCert string) string {
	if !enforce {
		return dsn
	}

	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" || !strings.HasPrefix(dsn, "postgres") {
		if !strings.Contains(dsn, "sslmode=") {
			sep := "?"
			if strings.Contains(dsn, "?") {
				sep = "&"
			}
			dsn += sep + "sslmode=" + mode
		}
		if rootCert != "" && !strings.Contains(dsn, "sslrootcert=") {
			sep := "?"
			if strings.Contains(dsn, "?") {
				sep = "&"
			}
			dsn += sep + "sslrootcert=" + url.QueryEscape(rootCert)
		}
		return dsn
	}

	q := u.Query()
	q.Set("sslmode", mode)
	if rootCert != "" && q.Get("sslrootcert") == "" {
		q.Set("sslrootcert", rootCert)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
