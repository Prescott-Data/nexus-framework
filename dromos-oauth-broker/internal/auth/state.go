package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type StateData struct {
	// WorkspaceID originates from Gateway user_id
	// and is reused as the user identifier during callback redirects
	WorkspaceID string    `json:"workspace_id"`
	ProviderID  string    `json:"provider_id"`
	Nonce       string    `json:"nonce"` // connection ID
	IAT         time.Time `json:"iat"`
}

// SignState signs state data with HMAC and returns base64 encoded state
func SignState(key []byte, data StateData) (string, error) {
	// Serialize data to JSON
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	// Create HMAC
	h := hmac.New(sha256.New, key)
	h.Write(dataBytes)
	signature := h.Sum(nil)

	// Combine data and signature
	state := fmt.Sprintf("%s.%s",
		base64.RawURLEncoding.EncodeToString(dataBytes),
		base64.RawURLEncoding.EncodeToString(signature))

	return state, nil
}

// VerifyState verifies and unpacks the signed state
func VerifyState(key []byte, state string) (*StateData, error) {
	// Split state into data and signature
	stateParts := strings.Split(state, ".")
	if len(stateParts) != 2 {
		return nil, fmt.Errorf("invalid state format")
	}

	dataBytes, err := base64.RawURLEncoding.DecodeString(stateParts[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode state data: %w", err)
	}

	signature, err := base64.RawURLEncoding.DecodeString(stateParts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode state signature: %w", err)
	}

	// Verify HMAC
	h := hmac.New(sha256.New, key)
	h.Write(dataBytes)
	expectedSignature := h.Sum(nil)

	if !hmac.Equal(signature, expectedSignature) {
		return nil, fmt.Errorf("invalid state signature")
	}

	// Unmarshal data
	var data StateData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state data: %w", err)
	}

	// Check if state is not too old (10 minutes)
	if time.Since(data.IAT) > 10*time.Minute {
		return nil, fmt.Errorf("state has expired")
	}

	return &data, nil
}
