package service

import (
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository"
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

	// For non-OAuth2 (API keys), we need a UserInfoEndpoint to test against
	if c.UserInfoEndpoint == "" {
		return "unknown"
	}

	// Fetch and decrypt the credentials
	credentials, _, err := w.connSvc.GetToken(ctx, c.ID)
	if err != nil {
		// GetToken can fail for internal reasons (decryption error, DB error).
		// Don't mark as expired — the credential might still be valid.
		log.Printf("ConnectionHealthWorker: Connection %s — failed to fetch token: %v", c.ID, err)
		return "degraded"
	}

	// Make a test request to the user_info_endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", c.UserInfoEndpoint, nil)
	if err != nil {
		return "unhealthy"
	}

	// Apply authentication using the same key extraction as validateCredentials
	// in credential.go. We use explicit credential keys to avoid accidentally
	// injecting unrelated map values (e.g., expires_at) as auth headers.
	switch c.AuthType {
	case "api_key":
		apiKey, _ := credentials["api_key"].(string)
		if apiKey == "" {
			log.Printf("ConnectionHealthWorker: Connection %s — api_key field missing from credentials", c.ID)
			return "degraded"
		}
		headerName := c.AuthHeader
		if headerName == "" {
			headerName = "Authorization"
		}
		if strings.ToLower(headerName) == "authorization" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		} else {
			req.Header.Set(headerName, apiKey)
		}

	case "basic_auth":
		username, _ := credentials["username"].(string)
		password, _ := credentials["password"].(string)
		if username == "" {
			log.Printf("ConnectionHealthWorker: Connection %s — username field missing from credentials", c.ID)
			return "degraded"
		}
		encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		req.Header.Set("Authorization", "Basic "+encoded)
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "unhealthy" // Network failure
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "expired" // The key is dead
	}

	if resp.StatusCode >= 500 {
		return "unhealthy" // Provider is having issues, don't mark as expired yet
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
