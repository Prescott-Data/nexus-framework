package server

import (
	"net/http"
	"strings"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
)

// ApiKeyMiddleware enforces X-API-Key header when requireKey is true.
func ApiKeyMiddleware(requireKey bool, allowedKeys map[string]struct{}) func(http.Handler) http.Handler {
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
			if _, ok := allowedKeys[key]; !ok {
				httputil.WriteError(w, http.StatusForbidden, "invalid_api_key", "invalid api key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
