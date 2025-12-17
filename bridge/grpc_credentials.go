package bridge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"dromos.io/bridge/internal/auth"
)

// BridgeCredentials implements credentials.PerRPCCredentials to automatically
// inject authentication metadata into gRPC calls managed by the Bridge.
type BridgeCredentials struct {
	oauthClient   OAuthClient
	connectionID  string
	refreshBuffer time.Duration
	logger        Logger

	// Cache the current token to avoid fetching on every RPC
	mu          sync.RWMutex
	cachedToken *Token
}

// NewBridgeCredentials creates a new PerRPCCredentials handler.
func NewBridgeCredentials(client OAuthClient, connectionID string, refreshBuffer time.Duration, logger Logger) *BridgeCredentials {
	return &BridgeCredentials{
		oauthClient:   client,
		connectionID:  connectionID,
		refreshBuffer: refreshBuffer,
		logger:        logger,
	}
}

// GetRequestMetadata is called by gRPC before sending a request.
func (c *BridgeCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	// 1. Get a valid token (cached or fresh)
	token, err := c.getValidToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get valid credentials: %w", err)
	}

	// 2. Use the Auth Engine to generate the metadata map
	md, err := auth.GetGRPCMetadata(token.Strategy, token.Credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to generate auth metadata: %w", err)
	}

	return md, nil
}

func (c *BridgeCredentials) getValidToken(ctx context.Context) (*Token, error) {
	c.mu.RLock()
	token := c.cachedToken
	c.mu.RUnlock()

	// If no token or expired, refresh
	if token == nil || c.isExpired(token) {
		c.mu.Lock()
		defer c.mu.Unlock()

		// Double check locking
		if c.cachedToken != nil && !c.isExpired(c.cachedToken) {
			return c.cachedToken, nil
		}

		c.logger.Info("Refreshing token for gRPC call", "connectionID", c.connectionID)
		newToken, err := c.oauthClient.GetToken(ctx, c.connectionID)
		if err != nil {
			return nil, err
		}
		c.cachedToken = newToken
		return newToken, nil
	}

	return token, nil
}

func (c *BridgeCredentials) isExpired(t *Token) bool {
	if t.ExpiresAt == 0 {
		return false // No expiry
	}
	// Check if we are within the buffer window
	return time.Now().After(time.Unix(t.ExpiresAt, 0).Add(-c.refreshBuffer))
}

// RequireTransportSecurity indicates whether TLS is required.
func (c *BridgeCredentials) RequireTransportSecurity() bool {
	// Return false to allow use with insecure connections (e.g. for testing or on trusted networks).
	// The user can still enforce TLS via grpc.WithTransportCredentials().
	return false
}
