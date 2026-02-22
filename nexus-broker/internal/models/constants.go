package models

// Auth Types
const (
	AuthTypeOAuth2      = "oauth2"
	AuthTypeAPIKey      = "api_key"
	AuthTypeBasicAuth   = "basic_auth"
	AuthTypeHeader      = "header"
	AuthTypeQueryParam  = "query_param"
	AuthTypeHMACPayload = "hmac_payload"
	AuthTypeAWSSigV4    = "aws_sigv4"
)

// Connection Statuses
const (
	ConnectionStatusActive    = "active"
	ConnectionStatusPending   = "pending"
	ConnectionStatusFailed    = "failed"
	ConnectionStatusAttention = "attention"
)

// Default Scopes
const (
	ScopeOpenID        = "openid"
	ScopeEmail         = "email"
	ScopeProfile       = "profile"
	ScopeOfflineAccess = "offline_access"
)
