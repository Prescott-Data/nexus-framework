package server

import (
	"net"
	"net/url"
	"os"
	"strings"
)

// IsReturnURLAllowed validates the return URL host against ALLOWED_RETURN_DOMAINS when ENFORCE_RETURN_URL=true.
func IsReturnURLAllowed(raw string) bool {
	enforce := strings.EqualFold(strings.TrimSpace(os.Getenv("ENFORCE_RETURN_URL")), "true")
	if !enforce {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Host
	if h, _, err := net.SplitHostPort(host); err == nil && h != "" {
		host = h
	}
	allowed := strings.Split(strings.TrimSpace(os.Getenv("ALLOWED_RETURN_DOMAINS")), ",")
	host = strings.ToLower(strings.TrimSpace(host))
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if a == host {
			return true
		}
		// Optional simple wildcard: *.example.com
		if strings.HasPrefix(a, "*.") {
			suf := strings.TrimPrefix(a, "*.")
			if strings.HasSuffix(host, "."+suf) {
				return true
			}
		}
	}
	return false
}
