package service

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/provider"
)

// ProviderHealthLookup provides read-only access to provider health status.
// This avoids importing the full Store and keeps the dependency narrow.
type ProviderHealthLookup interface {
	GetProfile(id uuid.UUID) (*provider.Profile, error)
}

// ConnectionHealthWorker polls for active connections and verifies their health
type ConnectionHealthWorker struct {
	connRepo       repository.ConnectionRepository
	connSvc        ConnectionService
	providerHealth ProviderHealthLookup
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

			// Before expiring a connection, cross-reference provider health.
			// If the provider itself is unhealthy, the refresh failure is likely
			// a transient upstream issue, not a revoked credential.
			if status == "expired" || status == "revoked" {
				if w.isProviderDown(c.ProviderID) {
					log.Printf("ConnectionHealthWorker: Connection %s refresh failed but provider %s is unhealthy — marking as unhealthy instead of expired", c.ID, c.ProviderName)
					status = "unhealthy"
				} else {
					log.Printf("ConnectionHealthWorker: Connection %s for provider %s is %s", c.ID, c.ProviderName, status)
					// Note: In a full implementation we should also write to the audit log here
					_ = w.connRepo.UpdateStatus(checkCtx, c.ID, "expired")
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

	profile, err := w.providerHealth.GetProfile(providerID)
	if err != nil {
		return false // Can't look up, assume provider is fine
	}

	return profile.HealthStatus == "unhealthy" || profile.HealthStatus == "degraded"
}

func (w *ConnectionHealthWorker) checkConnection(ctx context.Context, c *domain.ConnectionWithProvider) string {
	if c.AuthType == "oauth2" {
		// For OAuth2, attempt a token refresh. The service layer already has this logic.
		// If it succeeds, the refresh token is valid (healthy). 
		// If it fails with invalid_grant, it's expired.
		_, err := w.connSvc.Refresh(ctx, c.ID)
		if err != nil {
			// The caller (runChecks) will cross-reference provider health
			// before committing an "expired" status to the database.
			return "expired"
		}
		return "healthy"
	}

	// For non-OAuth2 (API keys), we need a UserInfoEndpoint to test against
	if c.UserInfoEndpoint == "" {
		return "unknown"
	}

	// Fetch and decrypt the credentials
	credentials, _, err := w.connSvc.GetToken(ctx, c.ID)
	if err != nil {
		return "expired" // Lost or corrupted token
	}

	// Make a test request to the user_info_endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", c.UserInfoEndpoint, nil)
	if err != nil {
		return "unhealthy"
	}

	// This is a simplified application of the strategy. A full implementation would 
	// use the bridge's `auth.ApplyAuthentication` but we are inside the broker here.
	// For API Key / Bearer, it's usually just a header.
	if c.AuthType == "api_key" || c.AuthType == "basic_auth" {
		// Assuming the token service returned it as a flat map
		for _, v := range credentials {
			if strVal, ok := v.(string); ok {
				// Very naive injection just for the health check.
				// In reality, this requires interpreting the provider's strategy config.
				req.Header.Set("Authorization", "Bearer "+strVal)
				req.Header.Set("X-API-Key", strVal)
			}
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
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
