package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// ValidateKey checks that a key value is set, valid base64, and decodes to
// exactly 32 bytes (AES-256). Returns the decoded key or an error with an
// actionable message including the environment variable name.
func ValidateKey(envName, value string) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf(
			"%s is not set. "+
				"This key is required and must be stable across restarts. "+
				"Generate one with: openssl rand -base64 32",
			envName,
		)
	}

	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf(
			"%s is not valid base64: %w. "+
				"Expected a base64-encoded 32-byte key. "+
				"Generate one with: openssl rand -base64 32",
			envName, err,
		)
	}

	if len(decoded) != 32 {
		return nil, fmt.Errorf(
			"%s decoded to %d bytes, expected exactly 32. "+
				"Generate a correct key with: openssl rand -base64 32",
			envName, len(decoded),
		)
	}

	return decoded, nil
}

// KeyFingerprint returns the first 12 hex characters of the key's SHA-256,
// safe to log for diagnostics without exposing any of the key itself.
func KeyFingerprint(key []byte) string {
	// A hash prefix, not a prefix of the key: the old form put 6 bytes of
	// the key itself in the startup log.
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])[:12]
}
