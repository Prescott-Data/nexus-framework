package service

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsPublicProbeTarget(t *testing.T) {
	blocked := []string{
		"127.0.0.1",       // loopback
		"127.1.2.3",       // rest of 127.0.0.0/8
		"::1",             // IPv6 loopback
		"0.0.0.0",         // unspecified
		"0.1.2.3",         // 0.0.0.0/8
		"10.0.0.5",        // RFC1918
		"172.16.4.4",      // RFC1918
		"192.168.1.1",     // RFC1918
		"169.254.169.254", // cloud metadata
		"fe80::1",         // IPv6 link-local
		"fc00::1",         // IPv6 unique-local
		"100.64.0.1",      // carrier-grade NAT
		"100.127.255.254", // upper end of CGNAT
		"224.0.0.1",       // multicast
		// IPv4-mapped IPv6 forms must not slip past the v4 checks.
		"::ffff:127.0.0.1",
		"::ffff:169.254.169.254",
		"::ffff:10.0.0.1",
	}
	for _, s := range blocked {
		if isPublicProbeTarget(net.ParseIP(s)) {
			t.Errorf("%s should be blocked as a probe target", s)
		}
	}

	allowed := []string{
		"8.8.8.8",
		"1.1.1.1",
		"140.82.121.4",    // github
		"2606:4700::1111", // public IPv6
		"100.63.255.255",  // just below CGNAT
		"100.128.0.1",     // just above CGNAT
		"172.32.0.1",      // just outside 172.16/12
		"11.0.0.1",        // just outside 10/8
	}
	for _, s := range allowed {
		if !isPublicProbeTarget(net.ParseIP(s)) {
			t.Errorf("%s should be allowed as a probe target", s)
		}
	}

	if isPublicProbeTarget(nil) {
		t.Error("nil IP must not be treated as a valid probe target")
	}
}

// The guard must apply at dial time, not merely as a string check on the URL,
// so that DNS rebinding and redirects to internal addresses are covered too.
func TestProbeClient_BlocksLoopbackDial(t *testing.T) {
	t.Setenv(AllowPrivateProbeTargetsEnv, "")

	var reached bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// httptest listens on 127.0.0.1, standing in for any internal host a
	// connecting user might point base_url at.
	if _, err := newProbeClient().Get(srv.URL); err == nil {
		t.Fatal("probe client dialed a loopback address; SSRF guard is not applied")
	}
	if reached {
		t.Fatal("request reached the blocked target")
	}
}

// Local development needs an opt-out, and it must be opt-in only.
func TestProbeClient_AllowsPrivateWhenExplicitlyEnabled(t *testing.T) {
	t.Setenv(AllowPrivateProbeTargetsEnv, "true")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resp, err := newProbeClient().Get(srv.URL)
	if err != nil {
		t.Fatalf("opt-out should permit private targets: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
}

// A typo or a truthy-looking value must not silently disable the guard.
func TestProbeClient_OptOutRequiresExactTrue(t *testing.T) {
	for _, tc := range []struct {
		value       string
		wantAllowed bool
	}{
		{"", false},
		{"1", false},
		{"yes", false},
		{"false", false},
		{"true", true},
		{"TRUE ", true}, // trimmed and case-folded
	} {
		t.Run("value="+tc.value, func(t *testing.T) {
			t.Setenv(AllowPrivateProbeTargetsEnv, tc.value)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			defer srv.Close()

			resp, err := newProbeClient().Get(srv.URL)
			if resp != nil {
				resp.Body.Close()
			}
			if tc.wantAllowed && err != nil {
				t.Fatalf("%q should enable the opt-out: %v", tc.value, err)
			}
			if !tc.wantAllowed && err == nil {
				t.Fatalf("%q must not enable the opt-out", tc.value)
			}
		})
	}
}
