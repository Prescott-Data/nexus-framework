package usecase

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type stateData struct {
	WorkspaceID string    `json:"workspace_id"`
	ProviderID  string    `json:"provider_id"`
	Nonce       string    `json:"nonce"`
	IAT         time.Time `json:"iat"`
}

func VerifyAndExtractConnectionID(key []byte, state string) (string, error) {
	parts := strings.Split(state, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid state format")
	}
	dataBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("state data decode: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("state sig decode: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(dataBytes)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", fmt.Errorf("invalid state signature")
	}
	var data stateData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return "", fmt.Errorf("state json: %w", err)
	}
	if time.Since(data.IAT) > 10*time.Minute {
		return "", fmt.Errorf("state expired")
	}
	if data.Nonce == "" {
		return "", fmt.Errorf("missing nonce")
	}
	return data.Nonce, nil
}
