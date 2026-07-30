package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
)

// Credential-validation probes are the one place where a connecting user
// controls the URL the broker fetches: self-hosted providers have no global
// api_base_url, so the instance URL arrives as the user's "base_url" credential
// (see effectiveBaseURL), and {field} placeholders in the endpoint are filled
// from user-supplied credentials (see renderEndpoint).
//
// Unguarded, that is server-side request forgery. The response body is never
// returned to the caller, but the outcome is still an oracle: a reachable host
// answering 401/403 surfaces as "credentials rejected" while an unreachable one
// surfaces as "validation unreachable", which is enough to map internal hosts
// and ports from outside the network.
//
// The broker is internet-hosted and every legitimate provider — including a
// customer's self-hosted Jenkins or Mattermost — is reachable over the public
// internet. It never needs to dial a private, loopback or link-local address,
// so refusing those costs nothing and closes the hole.

// AllowPrivateProbeTargetsEnv opts out of the guard for local development, where
// probe targets are typically stubs on 127.0.0.1. It must never be set in a
// deployed environment; it exists so `docker compose up` remains testable, and
// so tests that drive the real service against an httptest server can opt in
// explicitly rather than silently depending on the guard being absent.
const AllowPrivateProbeTargetsEnv = "NEXUS_ALLOW_PRIVATE_PROBE_TARGETS"

// errBlockedProbeTarget is returned by the dialer for a disallowed address. It
// reaches the broker log via validateCredentials, while the API response stays
// the generic "validation_unreachable" so the caller cannot distinguish a
// blocked address from an unreachable one.
type errBlockedProbeTarget struct{ addr string }

func (e *errBlockedProbeTarget) Error() string {
	return fmt.Sprintf("probe target %s is not a publicly routable address", e.addr)
}

// isPublicProbeTarget reports whether ip may be dialed by a validation probe.
func isPublicProbeTarget(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// Normalise IPv4-mapped IPv6 (::ffff:169.254.169.254) to its v4 form so the
	// checks below cannot be bypassed by expressing the address differently.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}

	switch {
	case ip.IsUnspecified(), // 0.0.0.0, ::
		ip.IsLoopback(),                // 127.0.0.0/8, ::1
		ip.IsPrivate(),                 // RFC1918, fc00::/7
		ip.IsLinkLocalUnicast(),        // 169.254.0.0/16 (cloud metadata), fe80::/10
		ip.IsLinkLocalMulticast(),      //
		ip.IsInterfaceLocalMulticast(), //
		ip.IsMulticast():
		return false
	}

	if v4 := ip.To4(); v4 != nil {
		// Carrier-grade NAT (100.64.0.0/10): not covered by IsPrivate, not
		// publicly routable either.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
		// 0.0.0.0/8 "this network" — only the exact address is IsUnspecified.
		if v4[0] == 0 {
			return false
		}
	}
	return true
}

// newProbeClient builds the HTTP client used for credential-validation probes.
//
// The address check runs in Dialer.Control, i.e. after DNS resolution and
// immediately before connect, against the address actually being dialed. That
// placement matters: checking the hostname up front would be defeated by DNS
// rebinding, and checking only the original URL would be defeated by a redirect
// to an internal address. Every connection — initial and redirected — passes
// through here.
//
// This client is deliberately not the shared caching client; see
// validationClient() for why probes must never be served from cache.
func newProbeClient() *http.Client {
	allowPrivate := strings.EqualFold(strings.TrimSpace(os.Getenv(AllowPrivateProbeTargetsEnv)), "true")

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if !allowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return &errBlockedProbeTarget{addr: address}
			}
			if ip := net.ParseIP(host); ip == nil || !isPublicProbeTarget(ip) {
				return &errBlockedProbeTarget{addr: host}
			}
			return nil
		}
	}

	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			MaxIdleConnsPerHost:   2,
		},
	}
}
