package bridge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-bridge/pkg/auth"
)

// BridgeCredentials implements credentials.PerRPCCredentials to automatically
// inject authentication metadata into gRPC calls managed by the Bridge.
type BridgeCredentials struct {
	oauthClient   auth.TokenProvider
	connectionID  string
	refreshBuffer time.Duration
	logger        Logger

	mu          sync.RWMutex
	cachedToken *auth.Token
}

// NewBridgeCredentials creates a new PerRPCCredentials handler.
func NewBridgeCredentials(client auth.TokenProvider, connectionID string, refreshBuffer time.Duration, logger Logger) *BridgeCredentials {
	return &BridgeCredentials{
		oauthClient:   client,
		connectionID:  connectionID,
		refreshBuffer: refreshBuffer,
		logger:        logger,
	}
}

// GetRequestMetadata is called by gRPC before sending a request.
func (c *BridgeCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	token, err := c.getValidToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get valid credentials: %w", err)
	}

	md, err := auth.GetGRPCMetadata(token.Strategy, token.Credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to generate auth metadata: %w", err)
	}

	return md, nil
}

func (c *BridgeCredentials) getValidToken(ctx context.Context) (*auth.Token, error) {
	c.mu.RLock()
	token := c.cachedToken
	c.mu.RUnlock()

	if token == nil || c.isExpired(token) {
		c.mu.Lock()
		defer c.mu.Unlock()

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

func (c *BridgeCredentials) isExpired(t *auth.Token) bool {
	if t.ExpiresAt == 0 {
		return false
	}
	return time.Now().After(time.Unix(t.ExpiresAt, 0).Add(-c.refreshBuffer))
}

// RequireTransportSecurity indicates whether TLS is required.
func (c *BridgeCredentials) RequireTransportSecurity() bool {
	return false
}
