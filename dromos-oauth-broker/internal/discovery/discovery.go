package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type OIDCMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

type cacheEntry struct {
	md        OIDCMetadata
	expiresAt time.Time
}

var (
	cacheMu              sync.RWMutex
	cache                = make(map[string]cacheEntry)
	hc                   = &http.Client{Timeout: 10 * time.Second}
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
func Discover(ctx context.Context, hint Hint) (OIDCMetadata, error) {
	start := time.Now()
	issuer := strings.TrimSpace(hint.Issuer)
	if issuer == "" {
		issuer = deriveIssuerFromAuthURL(hint.AuthURL)
	}
	if issuer == "" {
		metricsDiscoverTotal.WithLabelValues("error").Inc()
		return OIDCMetadata{}, errors.New("issuer not resolvable")
	}

	if md, ok := getCached(issuer); ok {
		return md, nil
	}

	wellKnown := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, _ := http.NewRequestWithContext(ctx, "GET", wellKnown, nil)
	resp, err := hc.Do(req)
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
	putCached(issuer, md, cacheTTL(resp))
	metricsDiscoverLatency.Observe(time.Since(start).Seconds())
	metricsDiscoverTotal.WithLabelValues("success").Inc()
	return md, nil
}

func cacheTTL(resp *http.Response) time.Duration {
	cc := resp.Header.Get("Cache-Control")
	if cc == "" {
		return time.Hour
	}
	// Very simple max-age parser
	for _, part := range strings.Split(cc, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "max-age=") {
			v := strings.TrimPrefix(part, "max-age=")
			if secs, err := time.ParseDuration(strings.TrimSpace(v) + "s"); err == nil {
				if secs > 0 {
					return secs
				}
			}
		}
	}
	return time.Hour
}

func getCached(issuer string) (OIDCMetadata, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	ce, ok := cache[issuer]
	if !ok || time.Now().After(ce.expiresAt) {
		return OIDCMetadata{}, false
	}
	return ce.md, true
}

func putCached(issuer string, md OIDCMetadata, ttl time.Duration) {
	cacheMu.Lock()
	cache[issuer] = cacheEntry{md: md, expiresAt: time.Now().Add(ttl)}
	cacheMu.Unlock()
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
