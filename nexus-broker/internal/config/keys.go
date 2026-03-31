package config

import (
	"encoding/base64"
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

// KeyFingerprint returns the first 8 characters of the base64-encoded key,
// safe to log for diagnostics without exposing the full secret.
func KeyFingerprint(key []byte) string {
	encoded := base64.StdEncoding.EncodeToString(key)
	if len(encoded) >= 8 {
		return encoded[:8] + "..."
	}
	return encoded
}
