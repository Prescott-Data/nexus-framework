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
	decoded, err := base64.RawURLEncoding.DecodeString(verifier)
	if err != nil || len(decoded) != 32 {
		t.Errorf("Verifier should decode to 32 bytes, got %d", len(decoded))
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
// no helper needed with DecodeString
