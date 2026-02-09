package auth

import (
	"testing"
	"time"
)

func TestSignAndVerifyState(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	data := StateData{
		WorkspaceID: "workspace-123",
		ProviderID:  "provider-456",
		Nonce:       "connection-789",
		IAT:         time.Now(),
	}

	// Sign the state
	signedState, err := SignState(key, data)
	if err != nil {
		t.Fatalf("SignState failed: %v", err)
	}

	// Verify the state
	verifiedData, err := VerifyState(key, signedState)
	if err != nil {
		t.Fatalf("VerifyState failed: %v", err)
	}

	// Check that data matches
	if verifiedData.WorkspaceID != data.WorkspaceID {
		t.Errorf("WorkspaceID mismatch: got %s, want %s", verifiedData.WorkspaceID, data.WorkspaceID)
	}
	if verifiedData.ProviderID != data.ProviderID {
		t.Errorf("ProviderID mismatch: got %s, want %s", verifiedData.ProviderID, data.ProviderID)
	}
	if verifiedData.Nonce != data.Nonce {
		t.Errorf("Nonce mismatch: got %s, want %s", verifiedData.Nonce, data.Nonce)
	}
}

func TestVerifyStateWithWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	for i := range key1 {
		key1[i] = byte(i)
		key2[i] = byte(i + 1)
	}

	data := StateData{
		WorkspaceID: "workspace-123",
		ProviderID:  "provider-456",
		Nonce:       "connection-789",
		IAT:         time.Now(),
	}

	// Sign with key1
	signedState, err := SignState(key1, data)
	if err != nil {
		t.Fatalf("SignState failed: %v", err)
	}

	// Try to verify with key2
	_, err = VerifyState(key2, signedState)
	if err == nil {
		t.Error("VerifyState should fail with wrong key")
	}
}

func TestVerifyStateExpired(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	// Create state data with timestamp from 15 minutes ago
	pastTime := time.Now().Add(-15 * time.Minute)
	data := StateData{
		WorkspaceID: "workspace-123",
		ProviderID:  "provider-456",
		Nonce:       "connection-789",
		IAT:         pastTime,
	}

	// Sign the state
	signedState, err := SignState(key, data)
	if err != nil {
		t.Fatalf("SignState failed: %v", err)
	}

	// Try to verify expired state
	_, err = VerifyState(key, signedState)
	if err == nil {
		t.Error("VerifyState should fail for expired state")
	}
}

func TestVerifyStateInvalidFormat(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	// Try to verify invalid state format
	_, err := VerifyState(key, "invalid-state-format")
	if err == nil {
		t.Error("VerifyState should fail for invalid format")
	}

	// Try to verify state with wrong number of parts
	_, err = VerifyState(key, "part1.part2.part3")
	if err == nil {
		t.Error("VerifyState should fail for wrong number of parts")
	}
}
