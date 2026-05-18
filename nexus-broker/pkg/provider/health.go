package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/discovery"
)

// HealthWorker periodically checks the health of all registered providers
type HealthWorker struct {
	store    *Store
	client   *http.Client
	interval time.Duration
}

func NewHealthWorker(store *Store, interval time.Duration) *HealthWorker {
	return &HealthWorker{
		store: store,
		client: &http.Client{
			Timeout: 10 * time.Second, // Prevent hanging requests
		},
		interval: interval,
	}
}

func (w *HealthWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Run once immediately on start
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

func (w *HealthWorker) runChecks(ctx context.Context) {
	profiles, err := w.store.GetAllProfiles()
	if err != nil {
		fmt.Printf("HealthWorker: failed to get profiles: %v\n", err)
		return
	}

	for _, p := range profiles {
		// Run each check in a goroutine to prevent one slow provider from blocking others
		go func(profile Profile) {
			status, message := w.checkProvider(ctx, profile)
			
			var msgPtr *string
			if message != "" {
				msgPtr = &message
			}

			if err := w.store.UpdateHealthStatus(profile.ID, status, msgPtr); err != nil {
				fmt.Printf("HealthWorker: failed to update status for %s: %v\n", profile.Name, err)
			}
		}(p)
	}
}

func (w *HealthWorker) checkProvider(ctx context.Context, p Profile) (string, string) {
	// If it's not OAuth2, perform a Tier 1 Reachability Check
	if p.AuthType != "oauth2" && p.AuthType != "" {
		return w.checkReachability(ctx, p)
	}

	tokenURL := ""

	// Tier 1 / Discovery setup
	if p.EnableDiscovery {
		var issuer string
		if p.Issuer != nil {
			issuer = *p.Issuer
		}
		
		// Attempt OIDC Discovery
		md, err := discovery.Discover(ctx, w.client, discovery.Hint{
			Issuer:  issuer,
			AuthURL: "", // We might not have AuthURL if discovery is enabled
		})

		if err != nil {
			return "unhealthy", fmt.Sprintf("OIDC Discovery failed: %v", err)
		}
		tokenURL = md.TokenEndpoint
	} else if p.TokenURL != nil {
		tokenURL = *p.TokenURL
	}

	if tokenURL == "" {
		return "unknown", "No token URL available to check"
	}

	// Tier 2: Configuration Validation (Deep Check)
	// We make a dummy token request.
	
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "dummy_code_for_health_check")
	
	if p.ClientID != nil {
		form.Set("client_id", *p.ClientID)
	}
	if p.ClientSecret != nil {
		form.Set("client_secret", *p.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "unhealthy", fmt.Sprintf("Failed to create request: %v", err)
	}
	
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := w.client.Do(req)
	if err != nil {
		return "unhealthy", fmt.Sprintf("Network error reaching token endpoint: %v", err)
	}
	defer resp.Body.Close()

	// Read a snippet of the body for the message
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	bodyStr := string(bodyBytes)

	// If the provider is healthy, it should recognize the valid format but reject the dummy code
	// Usually this means a 400 Bad Request or 401 Unauthorized with an OAuth error
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
		return "healthy", "Token endpoint reachable and returning expected OAuth error"
	}

	// If we get 5xx, the provider's API is down
	if resp.StatusCode >= 500 {
		return "unhealthy", fmt.Sprintf("Provider returned Server Error (%d)", resp.StatusCode)
	}

	// Any other status code is unexpected but not definitively "down"
	return "degraded", fmt.Sprintf("Unexpected status code %d: %s", resp.StatusCode, bodyStr)
}

func (w *HealthWorker) checkReachability(ctx context.Context, p Profile) (string, string) {
	targetURL := ""
	if p.UserInfoEndpoint != "" {
		targetURL = p.UserInfoEndpoint
	} else if p.APIBaseURL != "" {
		targetURL = p.APIBaseURL
	}

	if targetURL == "" {
		return "unknown", "No API Base URL or User Info Endpoint configured for reachability check"
	}

	// Try a HEAD request first, some APIs might reject it with 405 Method Not Allowed,
	// but even a 405 indicates the server is up.
	req, err := http.NewRequestWithContext(ctx, "HEAD", targetURL, nil)
	if err != nil {
		return "unhealthy", fmt.Sprintf("Failed to create request: %v", err)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return "unhealthy", fmt.Sprintf("Network error reaching endpoint: %v", err)
	}
	defer resp.Body.Close()

	// 5xx indicates a server error
	if resp.StatusCode >= 500 {
		return "unhealthy", fmt.Sprintf("Provider returned Server Error (%d)", resp.StatusCode)
	}

	// 2xx, 3xx, 4xx all indicate the server is actively responding
	return "healthy", fmt.Sprintf("Endpoint reachable (status %d)", resp.StatusCode)
}
