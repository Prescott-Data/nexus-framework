package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAPIKeyConfig(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "keys")
	if err := os.WriteFile(keyFile, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		env     map[string]string
		wantErr bool
		require bool
		keys    int
	}{
		{"default is off", nil, false, false, 0},
		{"on with inline keys", map[string]string{"GATEWAY_REQUIRE_API_KEY": "true", "GATEWAY_API_KEYS": "a, b"}, false, true, 2},
		{"on with a file", map[string]string{"GATEWAY_REQUIRE_API_KEY": "true", "GATEWAY_API_KEY_FILE": keyFile}, false, true, 0},
		{"on without keys is a startup error", map[string]string{"GATEWAY_REQUIRE_API_KEY": "true"}, true, true, 0},
		{"typo is a startup error, not off", map[string]string{"GATEWAY_REQUIRE_API_KEY": "tru", "GATEWAY_API_KEYS": "a"}, true, false, 0},
		{"bad interval", map[string]string{"GATEWAY_REQUIRE_API_KEY": "true", "GATEWAY_API_KEYS": "a", "GATEWAY_API_KEY_RELOAD_INTERVAL": "soon"}, true, true, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, k := range []string{"GATEWAY_REQUIRE_API_KEY", "GATEWAY_API_KEYS", "GATEWAY_API_KEY_FILE", "GATEWAY_API_KEYS_FILE", "GATEWAY_API_KEY_RELOAD_INTERVAL"} {
				t.Setenv(k, "")
			}
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			cfg, err := LoadAPIKeyConfig()
			if c.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Require != c.require || len(cfg.Keys) != c.keys {
				t.Fatalf("got require=%v keys=%d", cfg.Require, len(cfg.Keys))
			}
			src, err := cfg.Source()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Require && src == nil {
				t.Fatal("required config must produce a source")
			}
			if cfg.Require && !src.Contains(firstKey(cfg, "from-file")) {
				t.Fatal("configured key must be allowed")
			}
		})
	}
}

func firstKey(cfg APIKeyConfig, fallback string) string {
	for k := range cfg.Keys {
		return k
	}
	return fallback
}
