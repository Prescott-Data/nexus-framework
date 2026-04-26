package server

import (
	"net"
	"net/url"
	"strings"
)

// IsReturnURLAllowed validates the return URL host against the allowed domains
// when enforce is true. If enforce is false, all URLs are allowed.
func IsReturnURLAllowed(raw string, enforce bool, allowedDomains []string) bool {
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
	host = strings.ToLower(strings.TrimSpace(host))
	for _, a := range allowedDomains {
		if a == host {
			return true
		}
		if strings.HasPrefix(a, "*.") {
			suf := strings.TrimPrefix(a, "*.")
			if strings.HasSuffix(host, "."+suf) {
				return true
			}
		}
	}
	return false
}
