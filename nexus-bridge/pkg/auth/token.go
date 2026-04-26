package auth

import "context"

// Token holds the resolved authentication details for a connection.
type Token struct {
	Strategy    AuthStrategy
	Credentials Credentials
	ExpiresAt   int64 // Unix timestamp
}

// TokenProvider retrieves and refreshes tokens for managed connections.
// Implementations typically wrap the nexus-sdk HTTP client.
type TokenProvider interface {
	GetToken(ctx context.Context, connectionID string) (*Token, error)
	RefreshConnection(ctx context.Context, connectionID string) (*Token, error)
}
