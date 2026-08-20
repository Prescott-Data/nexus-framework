package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/auth"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault"
	"github.com/google/uuid"
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

	switch conn.AuthType {
	case "api_key", "basic_auth":
		// Providers explicitly marked as non-validatable (write-only ingestion
		// keys with no endpoint that can verify them) opt out of the probe.
		if parseValidationRule(conn.ProviderParams).Skip {
			break
		}
		// Static credentials must be verified before we mark the connection active.
		// Self-hosted providers have no global api_base_url; the user supplies
		// their instance URL as "base_url" in the capture payload.
		baseURL := effectiveBaseURL(conn.APIBaseURL, credentials)
		if conn.APIBaseURL == "" && baseURL != "" && !isValidBaseURL(baseURL) {
			return "", ErrBadRequest("invalid_base_url", "The provided base_url must be a valid http(s) URL")
		}
		// If the provider has no validation endpoint configured we cannot verify the
		// credential, so fail closed rather than reporting a false "connected" status.
		if baseURL == "" || conn.UserInfoEndpoint == "" {
			return "", ErrBadRequest("provider_not_validatable",
				"Provider is not configured for credential validation (missing api_base_url or user_info_endpoint)")
		}
		if err := s.validateCredentials(ctx, conn.AuthType, conn.AuthHeader, conn.APIBaseURL, conn.UserInfoEndpoint, conn.ProviderParams, credentials); err != nil {
			return "", err
		}
	default:
		// Other auth types keep best-effort validation when an endpoint is configured.
		baseURL := effectiveBaseURL(conn.APIBaseURL, credentials)
		if conn.UserInfoEndpoint != "" && baseURL != "" {
			if err := s.validateCredentials(ctx, conn.AuthType, conn.AuthHeader, conn.APIBaseURL, conn.UserInfoEndpoint, conn.ProviderParams, credentials); err != nil {
				return "", err
			}
		}
	}

	tokenJSON, err := json.Marshal(credentials)
	if err != nil {
		return "", ErrInternalWithErr(err, "credential_marshal_failed", "Failed to serialize credentials")
	}
	encryptedData, err := vault.Encrypt(s.encryptionKey, tokenJSON)
	if err != nil {
		return "", ErrInternalWithErr(err, "encryption_failed", "Failed to encrypt credentials")
	}

	if err := s.inTx(ctx, func(txCtx context.Context) error {
		if err := s.tokenRepo.Upsert(txCtx, &domain.Token{
			ConnectionID:  connID,
			EncryptedData: encryptedData,
		}); err != nil {
			return ErrInternalWithErr(err, "credential_store_failed", "Failed to store credentials")
		}
		if err := s.connRepo.UpdateStatus(txCtx, connID, "active"); err != nil {
			return ErrInternalWithErr(err, "status_update_failed", "Failed to update status")
		}
		if err := s.connRepo.DeactivateOtherActive(txCtx, stateData.WorkspaceID, conn.ProviderID, connID); err != nil {
			return ErrInternalWithErr(err, "deactivate_stale_failed", "Failed to deactivate stale connections")
		}
		return nil
	}); err != nil {
		return "", err
	}

	returnURL, err := url.Parse(conn.ReturnURL)
	if err != nil {
		return "", ErrInternalWithErr(err, "invalid_return_url", "Invalid return URL stored in connection")
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
	case "saml":
		return nil, ErrBadRequest("saml_not_refreshable", "SAML assertions cannot be refreshed; re-authentication is required")
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

		newTokens, statusCode, err := s.refreshTokens(ctx, tokenURL, clientID, clientSecret, refreshToken)
		if err != nil {
			if statusCode >= 400 && statusCode < 500 {
				s.connRepo.UpdateStatus(ctx, connectionID, "attention")
			}
			return &RefreshResponse{StatusCode: statusCode}, fmt.Errorf("refresh failed: %w", err)
		}

		// Merge the refreshed fields into the existing credential blob. Providers
		// like Google do NOT return a new refresh_token on refresh, so replacing
		// the stored blob outright would drop it and break the next refresh. We
		// only overwrite refresh_token when the provider actually rotated it.
		merged := current
		for k, v := range newTokens {
			merged[k] = v
		}
		if newRT, ok := newTokens["refresh_token"].(string); !ok || newRT == "" {
			merged["refresh_token"] = refreshToken
		}
		newTokens = merged

		tokenJSON, err := json.Marshal(newTokens)
		if err != nil {
			return nil, ErrInternalWithErr(err, "token_marshal_failed", "Failed to serialize refreshed tokens")
		}
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

// validationClient returns the HTTP client used for credential-validation
// probes.
//
// These MUST NOT go through the shared caching client: cachingTransport keys
// responses on the request URL alone, so the Authorization header is invisible
// to it. A probe with a valid credential would then satisfy every later probe
// for the same endpoint regardless of the credential supplied — accepting
// arbitrary keys for the whole TTL — and conversely a cached 401 would reject
// valid credentials. Probes are per-credential and must always hit the provider.
func (s *connectionService) validationClient() *http.Client {
	if s.probeClient != nil {
		return s.probeClient
	}
	// Tests construct connectionService directly with only httpClient set.
	return s.httpClient
}

func (s *connectionService) validateCredentials(ctx context.Context, authType, authHeader, apiBaseURL, userInfoEndpoint string, providerParams *json.RawMessage, credentials map[string]interface{}) error {
	// Self-hosted / instance-specific providers have no global api_base_url; the
	// user supplies their instance URL at connect time (stored as "base_url").
	baseURL := effectiveBaseURL(apiBaseURL, credentials)
	// Path-based providers carry the credential in the URL path via a {field}
	// template (e.g. Telegram /bot{api_key}/getMe); render it before building.
	endpoint := renderEndpoint(userInfoEndpoint, credentials)
	testURL := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(endpoint, "/")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return ErrInternalWithErr(err, "validation_request_failed", "Could not build credential-validation request")
	}

	// Authenticate the probe request exactly the way real requests are made:
	// resolve the same auth strategy the bridge uses at runtime (custom header
	// name/prefix, query-param, basic auth, ...) so non-standard providers can
	// actually be validated instead of always failing with a false 401.
	strat := resolveAuthStrategy(authType, authHeader, providerParams)
	if err := applyAuthStrategy(req, strat, credentials); err != nil {
		if err == errUnsupportedValidation {
			// Body-signing schemes (hmac/sigv4) can't be probed with a plain GET.
			// Skip validation rather than fail-open silently — log it.
			log.Printf("validateCredentials: strategy %q not validatable at connect time; skipping probe", strat.Type)
			return nil
		}
		return err
	}

	resp, err := s.validationClient().Do(req)
	if err != nil {
		// The broker could not reach the provider — this is NOT a credential
		// rejection. Surface it distinctly (502, logged) so it is not confused
		// with a bad key (e.g. missing egress from the broker to the provider).
		log.Printf("validateCredentials: could not reach provider at %s: %v", testURL, err)
		return ErrBadGatewayWithErr(err, "validation_unreachable", "Could not reach the provider to validate the credentials")
	}
	defer resp.Body.Close()

	// Interpret the response, including providers that signal failure with a
	// non-4xx status and an error body (e.g. Slack: 200 + {"ok":false}).
	return evaluateValidation(resp, parseValidationRule(providerParams))
}

func (s *connectionService) refreshTokens(ctx context.Context, tokenURL, clientID, clientSecret, refreshToken string) (map[string]interface{}, int, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
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
