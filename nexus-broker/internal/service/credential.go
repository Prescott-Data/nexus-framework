package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/auth"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault"
)

type RefreshResponse struct {
	Tokens     map[string]interface{}
	StatusCode int
}

func (s *connectionService) GetCaptureSchema(ctx context.Context, state string) (string, json.RawMessage, error) {
	stateData, err := auth.VerifyState(s.stateKey, state)
	if err != nil {
		return "", nil, ErrBadRequestWithErr(err, "invalid_state", "Invalid state")
	}

	providerID, err := uuid.Parse(stateData.ProviderID)
	if err != nil {
		return "", nil, ErrBadRequestWithErr(err, "invalid_provider_id", "Invalid provider ID in state")
	}

	p, err := s.providerStore.GetProfile(providerID)
	if err != nil {
		return "", nil, ErrNotFoundWithErr(err, "provider_not_found", "Provider not found")
	}

	var params map[string]json.RawMessage
	if p.Params != nil {
		if err := json.Unmarshal(*p.Params, &params); err != nil {
			return "", nil, ErrInternalWithErr(err, "invalid_params", "Failed to parse provider params")
		}
	}

	schema, ok := params["credential_schema"]
	if !ok {
		return "", nil, ErrNotFound("schema_not_found", "Credential schema not found for this provider")
	}

	return p.Name, schema, nil
}

func (s *connectionService) SaveCredential(ctx context.Context, state string, credentials map[string]interface{}) (string, error) {
	stateData, err := auth.VerifyState(s.stateKey, state)
	if err != nil {
		return "", ErrBadRequestWithErr(err, "invalid_state", "Invalid state")
	}

	connID, err := uuid.Parse(stateData.Nonce)
	if err != nil {
		return "", ErrBadRequestWithErr(err, "invalid_connection_id", "Invalid connection ID in state")
	}

	conn, err := s.connRepo.GetWithProvider(ctx, connID)
	if err != nil {
		return "", ErrNotFoundWithErr(err, "connection_not_found", "Connection not found")
	}

	if conn.UserInfoEndpoint != "" && conn.APIBaseURL != "" {
		if err := s.validateCredentials(conn.AuthType, conn.AuthHeader, conn.APIBaseURL, conn.UserInfoEndpoint, credentials); err != nil {
			return "", ErrBadRequestWithErr(err, "invalid_credentials", "Invalid credentials")
		}
	}

	tokenJSON, _ := json.Marshal(credentials)
	encryptedData, err := vault.Encrypt(s.encryptionKey, tokenJSON)
	if err != nil {
		return "", ErrInternalWithErr(err, "encryption_failed", "Failed to encrypt credentials")
	}

	err = s.tokenRepo.Upsert(ctx, &domain.Token{
		ConnectionID:  connID,
		EncryptedData: encryptedData,
	})
	if err != nil {
		return "", ErrInternalWithErr(err, "credential_store_failed", "Failed to store credentials")
	}

	if err := s.connRepo.UpdateStatus(ctx, connID, "active"); err != nil {
		return "", ErrInternalWithErr(err, "status_update_failed", "Failed to update status")
	}

	returnURL, err := url.Parse(conn.ReturnURL)
	if err != nil {
		return "", fmt.Errorf("invalid return_url")
	}

	query := returnURL.Query()
	query.Set("status", "success")
	query.Set("connection_id", connID.String())
	returnURL.RawQuery = query.Encode()

	return returnURL.String(), nil
}

func (s *connectionService) Refresh(ctx context.Context, connectionID uuid.UUID) (*RefreshResponse, error) {
	conn, err := s.connRepo.GetWithProvider(ctx, connectionID)
	if err != nil {
		return nil, ErrNotFoundWithErr(err, "connection_not_found", "Connection not active or not found")
	}

	if conn.Status != "active" {
		return nil, ErrBadRequest("connection_not_active", "Connection not active")
	}

	switch conn.AuthType {
	case "api_key", "basic_auth":
		return nil, ErrBadRequest("static_token", "Static credentials cannot be refreshed")
	case "oauth2", "":
		p, err := s.providerStore.GetProfile(conn.ProviderID)
		if err != nil {
			return nil, ErrNotFoundWithErr(err, "provider_not_found", "Provider not found")
		}

		token, err := s.tokenRepo.Get(ctx, connectionID)
		if err != nil {
			return nil, ErrNotFoundWithErr(err, "token_not_found", "Token not found")
		}

		plaintext, err := vault.Decrypt(s.encryptionKey, token.EncryptedData)
		if err != nil {
			return nil, ErrInternalWithErr(err, "decrypt_failed", "Failed to decrypt token")
		}

		var current map[string]interface{}
		if err := json.Unmarshal(plaintext, &current); err != nil {
			return nil, ErrInternalWithErr(err, "token_parse_failed", "Failed to parse token")
		}

		refreshToken, _ := current["refresh_token"].(string)
		if refreshToken == "" {
			return nil, ErrBadRequest("no_refresh_token", "No refresh token available")
		}

		tokenURL := ""
		if p.TokenURL != nil {
			tokenURL = *p.TokenURL
		}
		clientID := ""
		if p.ClientID != nil {
			clientID = *p.ClientID
		}
		clientSecret := ""
		if p.ClientSecret != nil {
			clientSecret = *p.ClientSecret
		}

		newTokens, statusCode, err := s.refreshTokens(tokenURL, clientID, clientSecret, refreshToken)
		if err != nil {
			if statusCode >= 400 && statusCode < 500 {
				s.connRepo.UpdateStatus(ctx, connectionID, "attention")
			}
			return &RefreshResponse{StatusCode: statusCode}, fmt.Errorf("refresh failed: %w", err)
		}

		tokenJSON, _ := json.Marshal(newTokens)
		encryptedData, err := vault.Encrypt(s.encryptionKey, tokenJSON)
		if err != nil {
			return nil, ErrInternalWithErr(err, "encryption_failed", "Failed to encrypt refreshed tokens")
		}

		var expiresAt *time.Time
		if expiresIn, ok := newTokens["expires_in"].(float64); ok {
			expiry := time.Now().Add(time.Duration(expiresIn) * time.Second)
			expiresAt = &expiry
		}

		err = s.tokenRepo.Upsert(ctx, &domain.Token{
			ConnectionID:  connectionID,
			EncryptedData: encryptedData,
			ExpiresAt:     expiresAt,
		})
		if err != nil {
			return nil, ErrInternalWithErr(err, "token_store_failed", "Failed to store refreshed token")
		}

		return &RefreshResponse{Tokens: newTokens, StatusCode: http.StatusOK}, nil

	default:
		return nil, ErrBadRequest("unsupported_auth_type", "Unsupported auth type")
	}
}

func (s *connectionService) validateCredentials(authType, authHeader, apiBaseURL, userInfoEndpoint string, credentials map[string]interface{}) error {
	testURL := strings.TrimRight(apiBaseURL, "/") + "/" + strings.TrimLeft(userInfoEndpoint, "/")

	req, err := http.NewRequest(http.MethodGet, testURL, nil)
	if err != nil {
		return fmt.Errorf("could not build validation request")
	}

	switch authType {
	case "api_key":
		apiKey, _ := credentials["api_key"].(string)
		if apiKey == "" {
			return fmt.Errorf("api_key credential is required")
		}
		headerName := authHeader
		if headerName == "" {
			headerName = "Authorization"
		}
		if strings.ToLower(headerName) == "authorization" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		} else {
			req.Header.Set(headerName, apiKey)
		}

	case "basic_auth":
		username, _ := credentials["username"].(string)
		password, _ := credentials["password"].(string)
		encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		req.Header.Set("Authorization", "Basic "+encoded)

	default:
		return nil
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach provider to validate credentials")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("credentials rejected by provider")
	}

	return nil
}

func (s *connectionService) refreshTokens(tokenURL, clientID, clientSecret, refreshToken string) (map[string]interface{}, int, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("token refresh failed: %s", string(body))
	}

	var tokens map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, resp.StatusCode, err
	}
	return tokens, resp.StatusCode, nil
}
