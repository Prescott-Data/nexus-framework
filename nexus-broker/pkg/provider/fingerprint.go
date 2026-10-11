package provider

import (
	"crypto/sha256"
	"encoding/hex"
)

// SecretFingerprint returns a short, non-reversible identifier for a client
// secret: the first 16 hex characters of its SHA-256. It is returned by the
// provider API so tooling can detect that a secret changed without the
// secret itself ever leaving the Broker. nexus-cli computes the same value
// from a manifest and compares.
func SecretFingerprint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])[:16]
}
