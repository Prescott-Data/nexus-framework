package config

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	gwmiddleware "github.com/Prescott-Data/nexus-framework/nexus-gateway/pkg/middleware"
)

// APIKeyConfig is who may call the Gateway. It mirrors the Broker's
// REQUIRE_API_KEY / API_KEYS / API_KEY_FILE family, prefixed GATEWAY_ so the
// two keys are never confused: BROKER_API_KEY is what the Gateway presents to
// the Broker; GATEWAY_API_KEYS is what callers present to the Gateway.
type APIKeyConfig struct {
	Require        bool
	Keys           map[string]struct{}
	Files          []string
	ReloadInterval time.Duration
}

// LoadAPIKeyConfig reads GATEWAY_REQUIRE_API_KEY, GATEWAY_API_KEYS,
// GATEWAY_API_KEY_FILE, GATEWAY_API_KEYS_FILE and GATEWAY_API_KEY_RELOAD_INTERVAL.
func LoadAPIKeyConfig() (APIKeyConfig, error) {
	cfg := APIKeyConfig{
		Keys:           map[string]struct{}{},
		ReloadInterval: 30 * time.Second,
	}
	// Strict: a typo must not silently start an unauthenticated Gateway.
	switch raw := strings.ToLower(strings.TrimSpace(os.Getenv("GATEWAY_REQUIRE_API_KEY"))); raw {
	case "", "false", "0":
		cfg.Require = false
	case "true", "1":
		cfg.Require = true
	default:
		return cfg, fmt.Errorf("GATEWAY_REQUIRE_API_KEY must be true or false, got %q", raw)
	}
	for _, key := range gwmiddleware.ParseAPIKeys(os.Getenv("GATEWAY_API_KEYS")) {
		cfg.Keys[key] = struct{}{}
	}
	for _, name := range []string{"GATEWAY_API_KEY_FILE", "GATEWAY_API_KEYS_FILE"} {
		for _, path := range strings.Split(os.Getenv(name), ",") {
			if path = strings.TrimSpace(path); path != "" {
				cfg.Files = append(cfg.Files, path)
			}
		}
	}
	if raw := strings.TrimSpace(os.Getenv("GATEWAY_API_KEY_RELOAD_INTERVAL")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return cfg, fmt.Errorf("GATEWAY_API_KEY_RELOAD_INTERVAL must be a positive duration, got %q", raw)
		}
		cfg.ReloadInterval = d
	}
	if cfg.Require && len(cfg.Keys) == 0 && len(cfg.Files) == 0 {
		return cfg, fmt.Errorf("GATEWAY_REQUIRE_API_KEY is true but no keys are configured; set GATEWAY_API_KEYS, GATEWAY_API_KEY_FILE or GATEWAY_API_KEYS_FILE")
	}
	return cfg, nil
}

// Source builds the key source, loading key files once so a bad path fails
// at startup. Logs loudly when enforcement is off: an unauthenticated
// Gateway lets anyone who can reach it mint tokens.
func (c APIKeyConfig) Source() (gwmiddleware.APIKeySource, error) {
	if !c.Require {
		log.Printf("WARNING: GATEWAY_REQUIRE_API_KEY is not true; the Gateway accepts requests from anyone who can reach it. Set GATEWAY_REQUIRE_API_KEY=true and GATEWAY_API_KEYS before exposing it.")
		return nil, nil
	}
	src, err := gwmiddleware.NewReloadingAPIKeySource(c.Keys, c.Files, c.ReloadInterval)
	if err != nil {
		return nil, err
	}
	if src.Len() == 0 {
		return nil, fmt.Errorf("GATEWAY_REQUIRE_API_KEY is true but the configured key files contain no keys")
	}
	log.Printf("Gateway API key enforcement: on (%d key(s), %d file(s), reload every %s)", src.Len(), len(c.Files), c.ReloadInterval)
	return src, nil
}
