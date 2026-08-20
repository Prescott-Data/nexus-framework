package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
)

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
// File contents are re-read at most once per reload interval, enabling mounted
// secret rotation without restarting the broker process.
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
func NewReloadingAPIKeySource(
	inlineKeys map[string]struct{},
	files []string,
	reloadInterval time.Duration,
) (*ReloadingAPIKeySource, error) {
	if reloadInterval <= 0 {
		return nil, fmt.Errorf("api key reload interval must be greater than zero")
	}

	source := &ReloadingAPIKeySource{
		inlineKeys:     copyAPIKeys(inlineKeys),
		files:          copyStrings(files),
		reloadInterval: reloadInterval,
		now:            time.Now,
	}
	if err := source.Reload(); err != nil {
		return nil, err
	}
	return source, nil
}

// Reload immediately re-reads configured files and swaps in the new allow-set.
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
	reloadDue := now.Sub(s.lastReload) >= s.reloadInterval
	s.mu.RUnlock()
	if !reloadDue {
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
		for _, key := range parseAPIKeys(string(raw)) {
			keys[key] = struct{}{}
		}
	}
	return keys, nil
}

// ApiKeyMiddleware enforces X-API-Key header when requireKey is true.
func ApiKeyMiddleware(requireKey bool, allowedKeys map[string]struct{}) func(http.Handler) http.Handler {
	return ApiKeySourceMiddleware(requireKey, NewStaticAPIKeySource(allowedKeys))
}

// ApiKeySourceMiddleware enforces X-API-Key header using a dynamic key source.
func ApiKeySourceMiddleware(requireKey bool, allowedKeys APIKeySource) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !requireKey {
				next.ServeHTTP(w, r)
				return
			}
			key := strings.TrimSpace(r.Header.Get("X-API-Key"))
			if key == "" {
				httputil.WriteError(w, http.StatusUnauthorized, "missing_api_key", "missing api key")
				return
			}
			if allowedKeys == nil || !allowedKeys.Contains(key) {
				httputil.WriteError(w, http.StatusForbidden, "invalid_api_key", "invalid api key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func parseAPIKeys(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t'
	})

	keys := make([]string, 0, len(parts))
	for _, part := range parts {
		key := strings.TrimSpace(part)
		if key != "" {
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

func copyStrings(values []string) []string {
	copied := make([]string, len(values))
	copy(copied, values)
	return copied
}
