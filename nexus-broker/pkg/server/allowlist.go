package server

import (
	"net"
	"net/http"
	"strings"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
)

// AllowlistMiddleware restricts access to the specified CIDRs when require is true.
func AllowlistMiddleware(require bool, allowedCIDRs string) func(http.Handler) http.Handler {
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
				httputil.WriteError(w, http.StatusForbidden, "access_denied", "Access denied")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func getClientIP(r *http.Request) net.IP {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := net.ParseIP(strings.TrimSpace(ips[0]))
			if ip != nil {
				return ip
			}
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return net.ParseIP("127.0.0.1")
	}
	return net.ParseIP(host)
}
