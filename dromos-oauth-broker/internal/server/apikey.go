package server

import (
	"net/http"
	"os"
	"strings"
)

// ApiKeyMiddleware enforces X-API-Key header when REQUIRE_API_KEY=true.
// Keys can be provided via API_KEYS (comma-separated) or API_KEY (single).
func ApiKeyMiddleware() func(http.Handler) http.Handler {
	require := strings.EqualFold(strings.TrimSpace(os.Getenv("REQUIRE_API_KEY")), "true")
	// Build an allow set
	allow := make(map[string]struct{})
	if v := strings.TrimSpace(os.Getenv("API_KEYS")); v != "" {
		for _, k := range strings.Split(v, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				allow[k] = struct{}{}
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv("API_KEY")); v != "" {
		allow[v] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !require {
				next.ServeHTTP(w, r)
				return
			}
			key := strings.TrimSpace(r.Header.Get("X-API-Key"))
			if key == "" {
				http.Error(w, "missing api key", http.StatusUnauthorized)
				return
			}
			if _, ok := allow[key]; !ok {
				http.Error(w, "invalid api key", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
