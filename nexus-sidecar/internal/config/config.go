package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultPort             = "8070"
	DefaultRequestBodyLimit = int64(10 * 1024 * 1024)
)

var routeNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

type Route struct {
	Name   string
	Target *url.URL
}

type Config struct {
	Port             string
	GatewayBaseURL   string
	Routes           map[string]Route
	TokenCacheTTL    time.Duration
	RequestBodyLimit int64
}

func LoadFromEnv() (Config, error) {
	cfg := Config{
		Port:             envOr("PORT", DefaultPort),
		GatewayBaseURL:   strings.TrimRight(strings.TrimSpace(os.Getenv("GATEWAY_BASE_URL")), "/"),
		TokenCacheTTL:    0,
		RequestBodyLimit: DefaultRequestBodyLimit,
	}

	if cfg.GatewayBaseURL == "" {
		return Config{}, fmt.Errorf("GATEWAY_BASE_URL is required")
	}
	if err := validateHTTPURL("GATEWAY_BASE_URL", cfg.GatewayBaseURL); err != nil {
		return Config{}, err
	}

	routes, err := ParseRoutes(os.Getenv("NEXUS_ROUTES"))
	if err != nil {
		return Config{}, err
	}
	cfg.Routes = routes

	if raw := strings.TrimSpace(os.Getenv("TOKEN_CACHE_TTL")); raw != "" {
		ttl, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TOKEN_CACHE_TTL is invalid: %w", err)
		}
		if ttl < 0 {
			return Config{}, fmt.Errorf("TOKEN_CACHE_TTL must be zero or greater")
		}
		cfg.TokenCacheTTL = ttl
	}

	if raw := strings.TrimSpace(os.Getenv("REQUEST_BODY_LIMIT")); raw != "" {
		limit, err := ParseByteSize(raw)
		if err != nil {
			return Config{}, fmt.Errorf("REQUEST_BODY_LIMIT is invalid: %w", err)
		}
		if limit <= 0 {
			return Config{}, fmt.Errorf("REQUEST_BODY_LIMIT must be greater than zero")
		}
		cfg.RequestBodyLimit = limit
	}

	return cfg, nil
}

func ParseRoutes(raw string) (map[string]Route, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("NEXUS_ROUTES is required")
	}

	routes := make(map[string]Route)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		name, targetRaw, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("route %q must use name=https://target format", part)
		}
		name = strings.Trim(strings.TrimSpace(name), "/")
		targetRaw = strings.TrimSpace(targetRaw)

		if !routeNamePattern.MatchString(name) {
			return nil, fmt.Errorf("route name %q must contain only letters, numbers, underscore, or hyphen", name)
		}
		if _, exists := routes[name]; exists {
			return nil, fmt.Errorf("duplicate route %q", name)
		}

		target, err := url.Parse(targetRaw)
		if err != nil {
			return nil, fmt.Errorf("route %q target is invalid: %w", name, err)
		}
		if target.Scheme != "http" && target.Scheme != "https" {
			return nil, fmt.Errorf("route %q target must use http or https", name)
		}
		if target.Host == "" {
			return nil, fmt.Errorf("route %q target must include a host", name)
		}

		routes[name] = Route{Name: name, Target: target}
	}

	if len(routes) == 0 {
		return nil, fmt.Errorf("NEXUS_ROUTES must contain at least one route")
	}
	return routes, nil
}

func ParseByteSize(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}

	lower := strings.ToLower(s)
	multiplier := int64(1)
	for _, suffix := range []struct {
		value string
		mult  int64
	}{
		{"gib", 1024 * 1024 * 1024},
		{"gb", 1000 * 1000 * 1000},
		{"gi", 1024 * 1024 * 1024},
		{"g", 1000 * 1000 * 1000},
		{"mib", 1024 * 1024},
		{"mb", 1000 * 1000},
		{"mi", 1024 * 1024},
		{"m", 1000 * 1000},
		{"kib", 1024},
		{"kb", 1000},
		{"ki", 1024},
		{"k", 1000},
	} {
		if strings.HasSuffix(lower, suffix.value) {
			multiplier = suffix.mult
			s = strings.TrimSpace(s[:len(s)-len(suffix.value)])
			break
		}
	}

	value, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return value * multiplier, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func validateHTTPURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", name, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", name)
	}
	if u.Host == "" {
		return fmt.Errorf("%s must include a host", name)
	}
	return nil
}
