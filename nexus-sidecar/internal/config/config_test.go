package config

import (
	"testing"
	"time"
)

func TestParseRoutes(t *testing.T) {
	routes, err := ParseRoutes("github=https://api.github.com, stripe=https://api.stripe.com/v1")
	if err != nil {
		t.Fatalf("ParseRoutes returned error: %v", err)
	}

	if got := routes["github"].Target.String(); got != "https://api.github.com" {
		t.Fatalf("github target = %q", got)
	}
	if got := routes["stripe"].Target.String(); got != "https://api.stripe.com/v1" {
		t.Fatalf("stripe target = %q", got)
	}
}

func TestParseRoutesRejectsInvalidInput(t *testing.T) {
	tests := []string{
		"",
		"github",
		"github=ftp://example.com",
		"github=http:///missing-host",
		"bad/name=https://example.com",
		"github=https://api.github.com,github=https://api2.github.com",
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseRoutes(raw); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseByteSize(t *testing.T) {
	tests := map[string]int64{
		"512":   512,
		"2KiB":  2 * 1024,
		"3kb":   3 * 1000,
		"4MiB":  4 * 1024 * 1024,
		"5mb":   5 * 1000 * 1000,
		"1GiB":  1024 * 1024 * 1024,
		"2gb":   2 * 1000 * 1000 * 1000,
		" 7 k ": 7 * 1000,
	}

	for raw, want := range tests {
		t.Run(raw, func(t *testing.T) {
			got, err := ParseByteSize(raw)
			if err != nil {
				t.Fatalf("ParseByteSize returned error: %v", err)
			}
			if got != want {
				t.Fatalf("ParseByteSize(%q) = %d, want %d", raw, got, want)
			}
		})
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("PORT", "9001")
	t.Setenv("GATEWAY_BASE_URL", "https://gateway.example.com/")
	t.Setenv("NEXUS_ROUTES", "github=https://api.github.com")
	t.Setenv("TOKEN_CACHE_TTL", "2m")
	t.Setenv("REQUEST_BODY_LIMIT", "1MiB")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if cfg.Port != "9001" {
		t.Fatalf("Port = %q", cfg.Port)
	}
	if cfg.GatewayBaseURL != "https://gateway.example.com" {
		t.Fatalf("GatewayBaseURL = %q", cfg.GatewayBaseURL)
	}
	if cfg.TokenCacheTTL != 2*time.Minute {
		t.Fatalf("TokenCacheTTL = %s", cfg.TokenCacheTTL)
	}
	if cfg.RequestBodyLimit != 1024*1024 {
		t.Fatalf("RequestBodyLimit = %d", cfg.RequestBodyLimit)
	}
}
