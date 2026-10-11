package config

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func validKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestValidateKey_Valid(t *testing.T) {
	encoded := validKey(t)
	key, err := ValidateKey("TEST_KEY", encoded)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key))
	}
}

func TestValidateKey_Empty(t *testing.T) {
	_, err := ValidateKey("ENCRYPTION_KEY", "")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	if !strings.Contains(err.Error(), "ENCRYPTION_KEY is not set") {
		t.Fatalf("error should mention env var name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "openssl rand -base64 32") {
		t.Fatalf("error should include generation hint, got: %v", err)
	}
}

func TestValidateKey_InvalidBase64(t *testing.T) {
	_, err := ValidateKey("STATE_KEY", "not!!valid!!base64$$")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if !strings.Contains(err.Error(), "STATE_KEY is not valid base64") {
		t.Fatalf("error should mention invalid base64, got: %v", err)
	}
}

func TestValidateKey_WrongLength_Short(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	_, err := ValidateKey("ENCRYPTION_KEY", short)
	if err == nil {
		t.Fatal("expected error for 16-byte key")
	}
	if !strings.Contains(err.Error(), "16 bytes") {
		t.Fatalf("error should report actual length, got: %v", err)
	}
	if !strings.Contains(err.Error(), "expected exactly 32") {
		t.Fatalf("error should state expected length, got: %v", err)
	}
}

func TestValidateKey_WrongLength_Long(t *testing.T) {
	long := base64.StdEncoding.EncodeToString(make([]byte, 64))
	_, err := ValidateKey("ENCRYPTION_KEY", long)
	if err == nil {
		t.Fatal("expected error for 64-byte key")
	}
	if !strings.Contains(err.Error(), "64 bytes") {
		t.Fatalf("error should report actual length, got: %v", err)
	}
}

func TestKeyFingerprint(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	fp := KeyFingerprint(raw)
	if len(fp) != 12 {
		t.Fatalf("fingerprint should be 12 hex chars, got %q", fp)
	}
	// Never a prefix of the key material itself, in any encoding.
	if strings.Contains(base64.StdEncoding.EncodeToString(raw), fp) {
		t.Fatalf("fingerprint %q must not be a slice of the encoded key", fp)
	}
	other := make([]byte, 32)
	copy(other, raw)
	other[31] ^= 1
	if KeyFingerprint(other) == fp {
		t.Fatal("a one-bit change must change the fingerprint")
	}
	if KeyFingerprint(raw) != fp {
		t.Fatal("fingerprint must be deterministic")
	}
}
