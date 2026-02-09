package oidcutil

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/oauth2"
)

// providerCache caches go-oidc Providers per issuer to reuse metadata and JWKS.
var (
	verifyTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "oidc_verifications_total",
		Help: "ID token verifications by result",
	}, []string{"result"})
	verifyLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "oidc_verification_duration_seconds",
		Help:    "Duration of ID token verification",
		Buckets: prometheus.DefBuckets,
	})
)

func init() {
	prometheus.MustRegister(verifyTotal, verifyLatency)
}

// randomString returns a base64url random string of n bytes.
func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// getProvider returns a cached OIDC provider for the issuer, creating it if needed.
func getProvider(ctx context.Context, client *http.Client, issuer string) (*gooidc.Provider, error) {
	iss := strings.TrimSpace(issuer)
	if iss == "" {
		return nil, errors.New("issuer is empty")
	}
	// Bind our caching client to the context
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)

	// The go-oidc library will now automatically use our client
	// (and its Redis cache) for its internal HTTP calls.
	prov, err := gooidc.NewProvider(ctx, iss)
	if err != nil {
		return nil, err
	}
	return prov, nil
}

// unverifiedIssuer extracts the iss claim from the raw JWT payload (without verification).
func unverifiedIssuer(rawIDToken string) (string, error) {
	parts := strings.Split(rawIDToken, ".")
	if len(parts) < 2 {
		return "", errors.New("invalid jwt format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return "", err
	}
	iss, _ := m["iss"].(string)
	if strings.TrimSpace(iss) == "" {
		return "", errors.New("issuer missing in token")
	}
	return iss, nil
}

// VerifyIDToken verifies the ID token against the discovered provider and clientID.
// It enforces signature, iss, aud, exp via go-oidc, and checks iat and nonce if provided.
func VerifyIDToken(ctx context.Context, client *http.Client, rawIDToken, clientID, expectedNonce string) (*gooidc.IDToken, error) {
	start := time.Now()
	if strings.TrimSpace(rawIDToken) == "" {
		verifyTotal.WithLabelValues("error").Inc()
		return nil, errors.New("id_token empty")
	}
	iss, err := unverifiedIssuer(rawIDToken)
	if err != nil {
		verifyTotal.WithLabelValues("error").Inc()
		return nil, err
	}
	prov, err := getProvider(ctx, client, iss)
	if err != nil {
		verifyTotal.WithLabelValues("error").Inc()
		return nil, err
	}
	verifier := prov.Verifier(&gooidc.Config{ClientID: clientID})
	idt, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		verifyTotal.WithLabelValues("error").Inc()
		return nil, err
	}
	// iat check (allow small clock skew)
	var claims struct {
		IAT   int64  `json:"iat"`
		Nonce string `json:"nonce"`
	}
	_ = idt.Claims(&claims)
	// iat optional but if present should not be in future > 120s
	if claims.IAT > 0 {
		if time.Unix(claims.IAT, 0).After(time.Now().Add(2 * time.Minute)) {
			verifyTotal.WithLabelValues("error").Inc()
			return nil, errors.New("id_token iat in the future")
		}
	}
	if strings.TrimSpace(expectedNonce) != "" {
		if strings.TrimSpace(claims.Nonce) == "" || claims.Nonce != expectedNonce {
			verifyTotal.WithLabelValues("error").Inc()
			return nil, errors.New("id_token nonce mismatch")
		}
	}
	verifyLatency.Observe(time.Since(start).Seconds())
	verifyTotal.WithLabelValues("success").Inc()
	return idt, nil
}
