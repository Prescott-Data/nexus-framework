package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// APIKeyHeader is the header callers present. It is the same header the
// SDKs already send and the same one the Broker checks.
const APIKeyHeader = "X-API-Key"

// APIKeySource checks whether an API key is currently allowed.
type APIKeySource interface {
	Contains(key string) bool
}

type staticAPIKeySource struct {
	keys map[string]struct{}
}

// NewStaticAPIKeySource creates an immutable API key source from an allow-set.
func NewStaticAPIKeySource(allowedKeys map[string]struct{}) APIKeySource {
	return &staticAPIKeySource{keys: copyAPIKeys(allowedKeys)}
}

func (s *staticAPIKeySource) Contains(key string) bool {
	_, ok := s.keys[key]
	return ok
}

// ReloadingAPIKeySource keeps inline keys and key files in a single allow-set.
// File contents are re-read at most once per reload interval, so a mounted
// secret can be rotated without restarting the gateway. Same semantics as the
// Broker's source.
type ReloadingAPIKeySource struct {
	inlineKeys     map[string]struct{}
	files          []string
	reloadInterval time.Duration
	now            func() time.Time

	mu         sync.RWMutex
	keys       map[string]struct{}
	lastReload time.Time
}

// NewReloadingAPIKeySource creates a reloadable source and performs an initial
// load so configuration errors fail during startup instead of on first request.
func NewReloadingAPIKeySource(inlineKeys map[string]struct{}, files []string, reloadInterval time.Duration) (*ReloadingAPIKeySource, error) {
	if reloadInterval <= 0 {
		return nil, fmt.Errorf("api key reload interval must be greater than zero")
	}
	source := &ReloadingAPIKeySource{
		inlineKeys:     copyAPIKeys(inlineKeys),
		files:          append([]string(nil), files...),
		reloadInterval: reloadInterval,
		now:            time.Now,
	}
	if err := source.Reload(); err != nil {
		return nil, err
	}
	return source, nil
}

// Reload re-reads the configured files and swaps in the new allow-set.
func (s *ReloadingAPIKeySource) Reload() error {
	next, err := s.load()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = next
	s.lastReload = s.now()
	return nil
}

// Len reports how many keys are currently allowed.
func (s *ReloadingAPIKeySource) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.keys)
}

func (s *ReloadingAPIKeySource) Contains(key string) bool {
	s.reloadIfDue()
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.keys[key]
	return ok
}

func (s *ReloadingAPIKeySource) reloadIfDue() {
	now := s.now()
	s.mu.RLock()
	due := now.Sub(s.lastReload) >= s.reloadInterval
	s.mu.RUnlock()
	if !due {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.Sub(s.lastReload) < s.reloadInterval {
		return
	}
	next, err := s.load()
	s.lastReload = now
	if err != nil {
		// Keep the last good set so a transient mount problem does not lock
		// every caller out, but say so: a revoked key stays valid until the
		// next successful reload. At most once per interval.
		log.Printf("WARNING: gateway api key reload failed; keeping the previous %d key(s) until the next attempt in %s: %v", len(s.keys), s.reloadInterval, err)
		return
	}
	s.keys = next
}

func (s *ReloadingAPIKeySource) load() (map[string]struct{}, error) {
	keys := copyAPIKeys(s.inlineKeys)
	for _, path := range s.files {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("load api key file %s: %w", path, err)
		}
		for _, key := range ParseAPIKeys(string(raw)) {
			keys[key] = struct{}{}
		}
	}
	return keys, nil
}

// APIKey enforces X-API-Key on every request except the exempt paths, which
// are the ones a browser reaches without the SDK: the health check, the
// OAuth callback proxy and the static-credential capture pages.
func APIKey(require bool, source APIKeySource, exemptPaths ...string) func(http.Handler) http.Handler {
	exempt := make(map[string]struct{}, len(exemptPaths))
	for _, p := range exemptPaths {
		exempt[p] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !require {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := exempt[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}
			key := strings.TrimSpace(r.Header.Get(APIKeyHeader))
			if key == "" {
				writeError(w, http.StatusUnauthorized, "missing_api_key", "missing api key")
				return
			}
			if source == nil || !source.Contains(key) {
				writeError(w, http.StatusForbidden, "invalid_api_key", "invalid api key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// APIKeyUnaryInterceptor is the gRPC counterpart. Direct gRPC clients send
// the key as metadata "x-api-key"; grpc-gateway forwards the HTTP header as
// "grpcgateway-x-api-key".
func APIKeyUnaryInterceptor(require bool, source APIKeySource) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if !require {
			return handler(ctx, req)
		}
		md, _ := metadata.FromIncomingContext(ctx)
		key := ""
		for _, name := range []string{"x-api-key", "grpcgateway-x-api-key"} {
			if v := md.Get(name); len(v) > 0 {
				key = strings.TrimSpace(v[0])
				break
			}
		}
		if key == "" {
			return nil, status.Error(codes.Unauthenticated, "missing api key")
		}
		if source == nil || !source.Contains(key) {
			return nil, status.Error(codes.PermissionDenied, "invalid api key")
		}
		return handler(ctx, req)
	}
}

// ParseAPIKeys splits a comma- or newline-separated list of keys.
func ParseAPIKeys(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t'
	})
	keys := make([]string, 0, len(parts))
	for _, part := range parts {
		if key := strings.TrimSpace(part); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func copyAPIKeys(keys map[string]struct{}) map[string]struct{} {
	copied := make(map[string]struct{}, len(keys))
	for key := range keys {
		copied[key] = struct{}{}
	}
	return copied
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message})
}
