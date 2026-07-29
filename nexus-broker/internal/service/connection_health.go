package service

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository"
	"github.com/google/uuid"
)

// ProviderHealthLookup provides read-only access to provider health status.
// Uses a narrow query that only fetches health_status, avoiding loading
// sensitive fields (client_secret, params, etc.) into worker memory.
type ProviderHealthLookup interface {
	GetHealthStatus(id uuid.UUID) (string, error)
}

// ConnectionHealthWorker polls for active connections and verifies their health
type ConnectionHealthWorker struct {
	connRepo       repository.ConnectionRepository
	connSvc        ConnectionService
	providerHealth ProviderHealthLookup
	httpClient     *http.Client
	interval       time.Duration
	batchSize      int
	maxConcurrency int
}

func NewConnectionHealthWorker(
	connRepo repository.ConnectionRepository,
	connSvc ConnectionService,
	providerHealth ProviderHealthLookup,
	interval time.Duration,
) *ConnectionHealthWorker {
	return &ConnectionHealthWorker{
		connRepo:       connRepo,
		connSvc:        connSvc,
		providerHealth: providerHealth,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
		interval:       interval,
		batchSize:      100, // Process 100 connections per interval
		maxConcurrency: 20,  // Limit to 20 concurrent health checks
	}
}

func (w *ConnectionHealthWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Run once immediately
	w.runChecks(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runChecks(ctx)
		}
	}
}

func (w *ConnectionHealthWorker) runChecks(ctx context.Context) {
	conns, err := w.connRepo.GetForHealthCheck(ctx, w.batchSize)
	if err != nil {
		log.Printf("ConnectionHealthWorker: failed to fetch connections: %v", err)
		return
	}

	if len(conns) == 0 {
		return
	}

	// Use a semaphore to bound concurrency
	sem := make(chan struct{}, w.maxConcurrency)
	var wg sync.WaitGroup

	for _, conn := range conns {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore slot

		go func(c *domain.ConnectionWithProvider) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore slot

			// A simple timeout context per check
			checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()

			status := w.checkConnection(checkCtx, c)

			// Only flip the connection's primary status to "expired" when we have
			// a definitive credential error AND the upstream provider is healthy.
			// For all other negative outcomes (unhealthy, degraded), we update
			// health_status but leave the connection's primary status untouched
			// to avoid overwriting states like "attention" set by the service layer.
			if status == "expired" {
				if w.isProviderDown(c.ProviderID) {
					log.Printf("ConnectionHealthWorker: Connection %s refresh failed but provider %s is unhealthy — marking as unhealthy instead of expired", c.ID, c.ProviderName)
					status = "unhealthy"
				} else {
					log.Printf("ConnectionHealthWorker: Connection %s for provider %s — credential definitively invalid, expiring", c.ID, c.ProviderName)
					if err := w.connRepo.UpdateStatus(checkCtx, c.ID, "expired"); err != nil {
						log.Printf("ConnectionHealthWorker: failed to expire connection %s — skipping health update to avoid inconsistent state: %v", c.ID, err)
						return
					}
				}
			}

			if err := w.connRepo.UpdateHealthStatus(checkCtx, c.ID, status); err != nil {
				log.Printf("ConnectionHealthWorker: failed to update health status for conn %s: %v", c.ID, err)
			}
		}(conn)
	}

	wg.Wait()
}

// isProviderDown checks whether the upstream provider is currently experiencing issues.
// Returns true if the provider's health status is "unhealthy" or "degraded".
func (w *ConnectionHealthWorker) isProviderDown(providerID uuid.UUID) bool {
	if w.providerHealth == nil {
		return false // No lookup available, assume provider is fine
	}

	status, err := w.providerHealth.GetHealthStatus(providerID)
	if err != nil {
		return false // Can't look up, assume provider is fine
	}

	return status == "unhealthy" || status == "degraded"
}

func (w *ConnectionHealthWorker) checkConnection(ctx context.Context, c *domain.ConnectionWithProvider) string {
	if c.AuthType == "oauth2" {
		return w.checkOAuth2Connection(ctx, c)
	}

	// Providers explicitly marked as non-validatable (validation.skip) have no
	// endpoint that can verify the key — their health cannot be probed.
	if parseValidationRule(c.ProviderParams).Skip {
		return "unknown"
	}

	// For non-OAuth2 (API keys), we need a UserInfoEndpoint to test against
	if c.UserInfoEndpoint == "" {
		return "unknown"
	}

	// Fetch and decrypt the credentials
	tokenResp, _, err := w.connSvc.GetToken(ctx, c.ID)
	if err != nil {
		// GetToken can fail for internal reasons (decryption error, DB error).
		// Don't mark as expired — the credential might still be valid.
		log.Printf("ConnectionHealthWorker: Connection %s — failed to fetch token: %v", c.ID, err)
		return "degraded"
	}
	// GetToken nests static credentials under "credentials"; OAuth spreads them
	// at the top level. Normalize so auth strategies find the right fields.
	creds := tokenResp
	if nested, ok := tokenResp["credentials"].(map[string]interface{}); ok {
		creds = nested
	}

	// Build the probe URL the same way connect-time validation does: resolve the
	// effective base URL (provider default or the user's self-hosted instance
	// URL) and render any path-based credential template.
	baseURL := effectiveBaseURL(c.APIBaseURL, creds)
	endpoint := renderEndpoint(c.UserInfoEndpoint, creds)
	if baseURL == "" && !(strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://")) {
		return "unknown"
	}
	testURL := endpoint
	if baseURL != "" {
		testURL = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(endpoint, "/")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
		return "unhealthy"
	}

	// Authenticate the probe using the same strategy engine as connect-time
	// validation and the bridge runtime, so health checks never diverge from
	// how the credential is actually used.
	strat := resolveAuthStrategy(c.AuthType, c.AuthHeader, c.ProviderParams)
	if err := applyAuthStrategy(req, strat, creds); err != nil {
		if err == errUnsupportedValidation {
			return "unknown" // body-signing schemes can't be probed with a GET
		}
		log.Printf("ConnectionHealthWorker: Connection %s — cannot apply auth: %v", c.ID, err)
		return "degraded"
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "unhealthy" // Network failure
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return "unhealthy" // Provider is having issues, don't mark as expired yet
	}

	// Status- and body-aware rejection check (e.g. Slack 200 + {"ok":false}).
	if err := evaluateValidation(resp, parseValidationRule(c.ProviderParams)); err != nil {
		return "expired" // The key is dead
	}

	return "healthy"
}

// checkOAuth2Connection inspects the RefreshResponse from the service layer to
// distinguish definitive credential errors from transient/internal failures.
//
// Status code mapping:
//
//	Success           → "healthy"
//	400/401           → "expired"  (invalid_grant, token revoked — definitive)
//	403               → "degraded" (scope issues — credential exists but limited)
//	5xx               → "unhealthy" (upstream issue — don't touch connection status)
//	Network/internal  → "degraded" (can't determine — don't touch connection status)
func (w *ConnectionHealthWorker) checkOAuth2Connection(ctx context.Context, c *domain.ConnectionWithProvider) string {
	resp, err := w.connSvc.Refresh(ctx, c.ID)
	if err == nil {
		return "healthy"
	}

	// Refresh returns a *RefreshResponse even on error, containing the upstream
	// status code. Use it to make a precise determination.
	if resp != nil && resp.StatusCode > 0 {
		switch {
		case resp.StatusCode == 400 || resp.StatusCode == 401:
			// Definitive: invalid_grant, token revoked, client deauthorized.
			// The service layer already set connection.status = "attention" for 4xx.
			// We return "expired" so runChecks can flip to "expired" if provider is healthy.
			return "expired"
		case resp.StatusCode == 403:
			// Partial revocation or scope downgrade. The refresh token may still be
			// valid but scopes are reduced. Don't expire the connection.
			return "degraded"
		case resp.StatusCode >= 500:
			// Upstream server error — transient. Don't touch the connection.
			return "unhealthy"
		default:
			// Unexpected status (e.g., 429 rate limit). Treat as transient.
			log.Printf("ConnectionHealthWorker: Connection %s — unexpected refresh status %d", c.ID, resp.StatusCode)
			return "degraded"
		}
	}

	// No response at all — network error, DNS failure, timeout, or internal service
	// error (decryption failure, missing provider, etc.). We can't determine whether
	// the credential is valid, so mark degraded and leave connection.status untouched.
	log.Printf("ConnectionHealthWorker: Connection %s — refresh error with no status code: %v", c.ID, err)
	return "degraded"
}
