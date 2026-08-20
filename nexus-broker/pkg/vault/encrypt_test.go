package vault

import (
	"encoding/base64"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte(`{"access_token":"token-123","refresh_token":"refresh-456"}`)

	ciphertext, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	decrypted, err := Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatalf("expected plaintext %q, got %q", plaintext, decrypted)
	}
}

func TestEncryptUsesUniqueNonce(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("same token payload")

	first, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("first Encrypt returned error: %v", err)
	}
	second, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("second Encrypt returned error: %v", err)
	}

	if first == second {
		t.Fatal("expected different ciphertexts for identical plaintext")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	ciphertext, err := Encrypt(key, []byte("token payload"))
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("ciphertext is not valid base64: %v", err)
	}
	data[len(data)-1] ^= 0x01
	tampered := base64.StdEncoding.EncodeToString(data)

	if _, err := Decrypt(key, tampered); err == nil {
		t.Fatal("expected tampered ciphertext to fail authentication")
	}
}

func TestEncryptRejectsInvalidKeyLength(t *testing.T) {
	if _, err := Encrypt(make([]byte, 31), []byte("token payload")); err == nil {
		t.Fatal("expected invalid key length error")
	}
}
