package server

import (
	"net"
	"net/http"
	"os"
	"strings"
)

// AllowlistMiddleware restricts access to specified CIDRs
func AllowlistMiddleware() func(http.Handler) http.Handler {
	require := strings.EqualFold(strings.TrimSpace(os.Getenv("REQUIRE_ALLOWLIST")), "true")
	allowedCIDRs := os.Getenv("ALLOWED_CIDRS")
	if allowedCIDRs == "" {
		// Default to localhost for development
		allowedCIDRs = "127.0.0.1/32,::1/128"
	}

	var nets []*net.IPNet
	for _, cidr := range strings.Split(allowedCIDRs, ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(cidr); err == nil {
			nets = append(nets, network)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !require {
				next.ServeHTTP(w, r)
				return
			}
			clientIP := getClientIP(r)

			allowed := false
			for _, network := range nets {
				if network.Contains(clientIP) {
					allowed = true
					break
				}
			}

			if !allowed {
				http.Error(w, "Access denied", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the real client IP
func getClientIP(r *http.Request) net.IP {
	// Check X-Forwarded-For header (if behind a trusted proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := net.ParseIP(strings.TrimSpace(ips[0]))
			if ip != nil {
				return ip
			}
		}
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return net.ParseIP("127.0.0.1") // fallback
	}
	return net.ParseIP(host)
}
