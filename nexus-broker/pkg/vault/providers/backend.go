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
	// Current returns the newest readable version of key and its reference,
	// or ErrNotFound. Used once per row written before version references
	// existed, to backfill its reference.
	Current(ctx context.Context, key string) (value, ref string, err error)
	// GetRef returns the exact version named by ref, as returned by Set, or
	// ErrNotFound when that version no longer exists.
	GetRef(ctx context.Context, ref string) (string, error)
	// Set adds a new version of the secret for key, creating the secret if
	// needed, and returns an immutable reference to that version. Earlier
	// versions stay readable until Retire, so a row that still points at one
	// keeps working while a rotation is in flight.
	Set(ctx context.Context, key, value string) (ref string, err error)
	// Retire destroys every version of key older than keepRef. Called after
	// the row referencing keepRef is committed.
	Retire(ctx context.Context, key, keepRef string) error
	// DiscardRef destroys the single version named by ref, leaving every
	// other version alone. Used when the row update that would have
	// referenced it definitely failed, so the value never becomes current.
	// Discarding a missing or already destroyed version is not an error.
	DiscardRef(ctx context.Context, ref string) error
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
