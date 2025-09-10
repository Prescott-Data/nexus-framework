package auth

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE failed: %v", err)
	}

	// Verifier should be base64 URL encoded
	if strings.ContainsAny(verifier, "+/=") {
		t.Error("Verifier contains invalid base64 URL characters")
	}

	// Challenge should be base64 URL encoded
	if strings.ContainsAny(challenge, "+/=") {
		t.Error("Challenge contains invalid base64 URL characters")
	}

	// Verifier should be 32 bytes when decoded
	verifierBytes := make([]byte, len(verifier)*6/8)
	n, err := decodeBase64URL(verifier, verifierBytes)
	if err != nil || n != 32 {
		t.Errorf("Verifier should decode to 32 bytes, got %d", n)
	}
}

func TestValidatePKCE(t *testing.T) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE failed: %v", err)
	}

	// Valid challenge should pass
	if !ValidatePKCE(verifier, challenge) {
		t.Error("Valid PKCE challenge should be accepted")
	}

	// Invalid challenge should fail
	if ValidatePKCE(verifier, "invalid-challenge") {
		t.Error("Invalid PKCE challenge should be rejected")
	}

	// Wrong verifier should fail
	if ValidatePKCE("wrong-verifier", challenge) {
		t.Error("Wrong verifier should be rejected")
	}
}

// decodeBase64URL is a helper to decode base64 URL strings for testing
func decodeBase64URL(s string, dst []byte) (int, error) {
	// Add padding if needed
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}

	return base64.RawURLEncoding.Decode(dst, []byte(s))
}
