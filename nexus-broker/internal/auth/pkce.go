package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// GeneratePKCE generates a code verifier and code challenge for PKCE
func GeneratePKCE() (verifier string, challenge string, err error) {
	// Generate 32 bytes of random data
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return "", "", err
	}

	// Base64 URL encode the verifier
	verifier = base64.RawURLEncoding.EncodeToString(verifierBytes)

	// Create code challenge by hashing the verifier with SHA256
	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])

	return verifier, challenge, nil
}

// ValidatePKCE validates that a code verifier matches a code challenge
func ValidatePKCE(verifier, challenge string) bool {
	hash := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(hash[:])
	return strings.TrimRight(expectedChallenge, "=") == strings.TrimRight(challenge, "=")
}
