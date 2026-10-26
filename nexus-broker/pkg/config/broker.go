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

	// Subject binding: whom a credential may be released for. "optional" (the
	// default) refuses a request that names a subject the connection does not
	// belong to; "required" also refuses one that names nobody; "off" ignores
	// the subject. See docs/reference/security-model.md.
	SubjectBinding string

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

	// Where OAuth2 provider client secrets are stored. "internal" keeps them
	// in provider_profiles.client_secret; an external backend moves them out
	// of the database. See pkg/vault/providers.
	SecretBackend   string
	GCPProjectID    string
	GCPSecretPrefix string
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
		SubjectBinding:   strings.ToLower(envOr("SUBJECT_BINDING", "optional")),

		EnforceDBSSL:  envBool("ENFORCE_DB_SSL"),
		DBSSLMode:     envOr("DB_SSLMODE", "require"),
		DBSSLRootCert: strings.TrimSpace(os.Getenv("DB_SSLROOTCERT")),

		AutoMigrate:   envBoolDefault("AUTO_MIGRATE", true),
		MigrationsDir: envOr("MIGRATIONS_DIR", "/app/migrations"),

		SecretBackend:   strings.ToLower(strings.TrimSpace(envOr("SECRET_BACKEND", "internal"))),
		GCPProjectID:    strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")),
		GCPSecretPrefix: envOr("GCP_SECRET_PREFIX", "nexus-providers"),
	}

	switch cfg.SecretBackend {
	case "internal", "gcp-secret-manager":
	default:
		return nil, fmt.Errorf("SECRET_BACKEND must be one of: internal, gcp-secret-manager (got %q)", cfg.SecretBackend)
	}
	if cfg.SecretBackend == "gcp-secret-manager" {
		if err := validateSecretPrefix(cfg.GCPSecretPrefix); err != nil {
			return nil, err
		}
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

	switch cfg.SubjectBinding {
	case "off", "optional", "required":
	default:
		return nil, fmt.Errorf("SUBJECT_BINDING must be off, optional or required, not %q", cfg.SubjectBinding)
	}
	return cfg, nil
}

// validateSecretPrefix enforces Secret Manager's secret ID charset on the
// prefix so a bad value fails at boot rather than on the first registration.
// Secret Manager IDs are limited to 255 characters and the broker appends
// "-" plus a 36-character UUID, so the prefix itself may be at most 218.
const maxSecretPrefixLen = 255 - 1 - 36

func validateSecretPrefix(prefix string) error {
	if prefix == "" {
		return fmt.Errorf("GCP_SECRET_PREFIX must not be empty")
	}
	if len(prefix) > maxSecretPrefixLen {
		return fmt.Errorf("GCP_SECRET_PREFIX may be at most %d characters so that <prefix>-<provider id> fits Secret Manager's 255-character limit (got %d)", maxSecretPrefixLen, len(prefix))
	}
	for _, r := range prefix {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isAlnum && r != '-' && r != '_' {
			return fmt.Errorf("GCP_SECRET_PREFIX may only contain letters, digits, '-' and '_' (got %q)", prefix)
		}
	}
	return nil
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
