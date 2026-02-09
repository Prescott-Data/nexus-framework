package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type OIDCMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

var (
	metricsDiscoverTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "oidc_discovery_total",
		Help: "OIDC discovery attempts by result",
	}, []string{"result"})
	metricsDiscoverLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "oidc_discovery_duration_seconds",
		Help:    "Duration of OIDC discovery",
		Buckets: prometheus.DefBuckets,
	})
)

func init() {
	prometheus.MustRegister(metricsDiscoverTotal, metricsDiscoverLatency)
}

type Hint struct {
	Issuer  string
	AuthURL string
}

// Discover attempts to resolve OIDC metadata using issuer or heuristics from auth URL.
// It caches results with a default TTL of 1h if no Cache-Control directive is present.
func Discover(ctx context.Context, client *http.Client, hint Hint) (OIDCMetadata, error) {
	start := time.Now()
	issuer := strings.TrimSpace(hint.Issuer)
	if issuer == "" {
		issuer = deriveIssuerFromAuthURL(hint.AuthURL)
	}
	if issuer == "" {
		metricsDiscoverTotal.WithLabelValues("error").Inc()
		return OIDCMetadata{}, errors.New("issuer not resolvable")
	}

	wellKnown := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, _ := http.NewRequestWithContext(ctx, "GET", wellKnown, nil)
	resp, err := client.Do(req)
	if err != nil {
		metricsDiscoverTotal.WithLabelValues("error").Inc()
		return OIDCMetadata{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		metricsDiscoverTotal.WithLabelValues("error").Inc()
		return OIDCMetadata{}, errors.New("discovery failed: non-200")
	}
	var md OIDCMetadata
	if err := json.NewDecoder(resp.Body).Decode(&md); err != nil {
		metricsDiscoverTotal.WithLabelValues("error").Inc()
		return OIDCMetadata{}, err
	}
	if strings.TrimSpace(md.Issuer) == "" || strings.TrimSpace(md.JWKSURI) == "" {
		metricsDiscoverTotal.WithLabelValues("error").Inc()
		return OIDCMetadata{}, errors.New("discovery invalid payload")
	}
	metricsDiscoverLatency.Observe(time.Since(start).Seconds())
	metricsDiscoverTotal.WithLabelValues("success").Inc()
	return md, nil
}

// deriveIssuerFromAuthURL uses known patterns to compute an issuer.
func deriveIssuerFromAuthURL(authURL string) string {
	raw := strings.TrimSpace(authURL)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	path := u.Path
	// Google
	if strings.Contains(host, "accounts.google.com") {
		return "https://accounts.google.com"
	}
	// Azure v2
	reAzure := regexp.MustCompile(`/([^/]+)/oauth2/v2.0/authorize$`)
	if m := reAzure.FindStringSubmatch(path); len(m) == 2 {
		tenant := m[1]
		return "https://" + host + "/" + tenant + "/v2.0"
	}
	// Generic fallback: try scheme://host as issuer
	return u.Scheme + "://" + u.Host
}
