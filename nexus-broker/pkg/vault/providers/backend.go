// Package providers defines where provider client secrets are stored.
//
// The broker keeps provider metadata (URLs, scopes, auth type) in Postgres.
// A Backend decides where the client_secret for an OAuth2 provider lives.
// The default is the Postgres column itself; an external backend moves the
// secret out of the database so that it never sits next to the metadata or
// in database backups.
package providers

import (
	"context"
	"errors"
)

// Kind names a backend implementation. It is the value of SECRET_BACKEND.
type Kind string

const (
	// KindInternal keeps client secrets in provider_profiles.client_secret.
	KindInternal Kind = "internal"
	// KindGCPSecretManager stores one Secret Manager secret per provider.
	KindGCPSecretManager Kind = "gcp-secret-manager"
)

// ErrNotFound is returned by Get when no secret exists for the key.
var ErrNotFound = errors.New("provider secret not found")

// Backend stores provider client secrets outside the database.
//
// Keys are opaque to callers; the store passes the provider UUID. Each
// implementation maps the key to its own naming scheme. Keys are stable for
// the life of a provider, so renaming a provider never orphans its secret.
type Backend interface {
	// Kind reports which implementation this is, for logs and startup checks.
	Kind() Kind
	// Get returns the secret for key, or ErrNotFound.
	Get(ctx context.Context, key string) (string, error)
	// Set creates or replaces the secret for key.
	Set(ctx context.Context, key, value string) error
	// Delete removes the secret for key. Deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
	// Ping verifies the backend is reachable and the caller is authorised.
	// It runs once at broker startup so a misconfigured backend fails fast
	// instead of failing on the first OAuth exchange.
	Ping(ctx context.Context) error
}

// ParseKind validates a SECRET_BACKEND value. An empty value is internal.
func ParseKind(raw string) (Kind, error) {
	switch Kind(raw) {
	case "", KindInternal:
		return KindInternal, nil
	case KindGCPSecretManager:
		return KindGCPSecretManager, nil
	default:
		return "", errors.New("SECRET_BACKEND must be one of: internal, gcp-secret-manager")
	}
}
