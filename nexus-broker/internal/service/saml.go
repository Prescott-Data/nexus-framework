package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/url"
	"time"

	"github.com/crewjam/saml"
	"github.com/google/uuid"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/auth"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/provider"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/samlutil"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/server"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault"
)

func (s *connectionService) createSAMLConsentSpec(ctx context.Context, req CreateConsentRequest, providerID uuid.UUID, p *provider.Profile) (*ConsentSpecResponse, error) {
	connID := uuid.New()
	conn := &domain.Connection{
		ID:           connID,
		WorkspaceID:  req.WorkspaceID,
		ProviderID:   providerID,
		CodeVerifier: sql.NullString{},
		Scopes:       req.Scopes,
		ReturnURL:    req.ReturnURL,
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}

	if err := s.connRepo.Create(ctx, conn); err != nil {
		return nil, ErrInternalWithErr(err, "connection_create_failed", "Failed to create connection")
	}

	sp, err := s.buildSAMLServiceProvider(providerID, p)
	if err != nil {
		return nil, ErrInternalWithErr(err, "saml_sp_build_failed", "Failed to build SAML service provider")
	}

	idpURL := sp.GetSSOBindingLocation(saml.HTTPRedirectBinding)
	if idpURL == "" {
		return nil, ErrBadRequest("saml_sso_url_missing", "SAML IdP SSO URL is missing")
	}

	authReq, err := sp.MakeAuthenticationRequest(idpURL, saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		return nil, ErrInternalWithErr(err, "saml_authn_request_failed", "Failed to create SAML AuthnRequest")
	}

	stateData := auth.StateData{
		WorkspaceID:   req.WorkspaceID,
		ProviderID:    req.ProviderID,
		Nonce:         connID.String(),
		SAMLRequestID: authReq.ID,
		IAT:           time.Now(),
	}
	signedState, err := auth.SignState(s.stateKey, stateData)
	if err != nil {
		return nil, ErrInternalWithErr(err, "state_sign_failed", "Failed to sign state")
	}

	redirectURL, err := authReq.Redirect(signedState, sp)
	if err != nil {
		return nil, ErrInternalWithErr(err, "saml_redirect_failed", "Failed to build SAML redirect URL")
	}

	return &ConsentSpecResponse{
		AuthURL:    redirectURL.String(),
		State:      signedState,
		Scopes:     req.Scopes,
		ProviderID: req.ProviderID,
	}, nil
}

func (s *connectionService) GetSAMLMetadata(ctx context.Context, providerID uuid.UUID) ([]byte, error) {
	p, err := s.providerStore.GetProfile(providerID)
	if err != nil {
		return nil, ErrNotFoundWithErr(err, "provider_not_found", "Provider not found")
	}
	if p.AuthType != "saml" {
		return nil, ErrBadRequest("invalid_auth_type", "Provider is not a SAML provider")
	}

	sp, err := s.buildSAMLServiceProvider(providerID, p)
	if err != nil {
		return nil, ErrInternalWithErr(err, "saml_sp_build_failed", "Failed to build SAML service provider")
	}

	metadata := sp.Metadata()
	data, err := xml.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, ErrInternalWithErr(err, "metadata_marshal_failed", "Failed to marshal SAML metadata")
	}
	return append([]byte(xml.Header), data...), nil
}

func (s *connectionService) ExchangeSAMLResponse(ctx context.Context, r *http.Request) (string, error) {
	if err := r.ParseForm(); err != nil {
		return "", ErrBadRequestWithErr(err, "invalid_request", "Failed to parse SAML form")
	}
	if r.FormValue("SAMLResponse") == "" {
		return "", ErrBadRequest("missing_saml_response", "Missing SAMLResponse")
	}

	relayState := r.FormValue("RelayState")
	if relayState == "" {
		return "", ErrBadRequest("missing_relay_state", "Missing RelayState")
	}

	stateData, err := auth.VerifyState(s.stateKey, relayState)
	if err != nil {
		return "", ErrBadRequestWithErr(err, "invalid_state", "Invalid state")
	}
	if stateData.SAMLRequestID == "" {
		return "", ErrBadRequest("missing_saml_request_id", "RelayState is missing the SAML request binding")
	}

	connID, err := uuid.Parse(stateData.Nonce)
	if err != nil {
		return "", ErrBadRequestWithErr(err, "invalid_connection_id", "Invalid connection ID in state")
	}

	conn, err := s.connRepo.GetPending(ctx, connID)
	if err != nil {
		return "", ErrNotFoundWithErr(err, "connection_not_found", "Connection not found or expired")
	}

	// Validate the redirect target before any credential is stored.
	if !server.IsReturnURLAllowed(conn.ReturnURL, s.enforceReturnURL, s.allowedReturnDomains) {
		return "", ErrBadRequest("return_url_not_allowed", "return_url not allowed")
	}
	returnURL, err := url.Parse(conn.ReturnURL)
	if err != nil {
		return "", ErrInternalWithErr(err, "invalid_return_url", "Invalid return_url")
	}

	p, err := s.providerStore.GetProfile(conn.ProviderID)
	if err != nil {
		return "", ErrNotFoundWithErr(err, "provider_not_found", "Provider not found")
	}
	if p.AuthType != "saml" {
		return "", ErrBadRequest("invalid_auth_type", "Provider is not a SAML provider")
	}

	sp, err := s.buildSAMLServiceProvider(conn.ProviderID, p)
	if err != nil {
		return "", ErrInternalWithErr(err, "saml_sp_build_failed", "Failed to build SAML service provider")
	}

	assertion, err := sp.ParseResponse(r, []string{stateData.SAMLRequestID})
	if err != nil {
		_ = s.connRepo.UpdateStatus(ctx, connID, "failed")
		return "", NewServiceError(http.StatusUnauthorized, err, "saml_verification_failed", "SAML assertion validation failed")
	}

	credentials, expiresAt := samlCredentials(assertion)
	tokenJSON, err := json.Marshal(credentials)
	if err != nil {
		return "", ErrInternalWithErr(err, "token_marshal_failed", "Failed to serialize SAML attributes")
	}
	encryptedData, err := vault.Encrypt(s.encryptionKey, tokenJSON)
	if err != nil {
		return "", ErrInternalWithErr(err, "encryption_failed", "Failed to encrypt SAML attributes")
	}

	if err := s.inTx(ctx, func(txCtx context.Context) error {
		if err := s.tokenRepo.Upsert(txCtx, &domain.Token{ConnectionID: connID, EncryptedData: encryptedData, ExpiresAt: expiresAt}); err != nil {
			return ErrInternalWithErr(err, "token_store_failed", "Failed to store SAML attributes")
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

	query := returnURL.Query()
	query.Set("status", "success")
	query.Set("connection_id", connID.String())
	query.Set("provider", p.Name)
	returnURL.RawQuery = query.Encode()
	return returnURL.String(), nil
}

func (s *connectionService) buildSAMLServiceProvider(providerID uuid.UUID, p *provider.Profile) (*saml.ServiceProvider, error) {
	acsURL, _ := url.JoinPath(s.baseURL, "/saml/acs")
	metadataURL, _ := url.JoinPath(s.baseURL, "/saml/metadata", providerID.String())
	return samlutil.BuildServiceProvider(samlutil.ServiceProviderConfig{
		SPEntityID:  providerString(p.SAMLSPEntityID),
		ACSURL:      acsURL,
		MetadataURL: metadataURL,
		IDPEntityID: providerString(p.SAMLIdpEntityID),
		IDPSSOURL:   providerString(p.SAMLIdpSSOURL),
		IDPX509Cert: providerString(p.SAMLIdpX509Cert),
	})
}

func samlCredentials(assertion *saml.Assertion) (map[string]interface{}, *time.Time) {
	attributes := make(map[string]interface{})
	if assertion != nil {
		for _, statement := range assertion.AttributeStatements {
			for _, attr := range statement.Attributes {
				values := make([]string, 0, len(attr.Values))
				for _, value := range attr.Values {
					values = append(values, value.Value)
				}
				switch len(values) {
				case 0:
					continue
				case 1:
					attributes[attr.Name] = values[0]
				default:
					attributes[attr.Name] = values
				}
			}
		}
	}

	nameID := ""
	if assertion != nil && assertion.Subject != nil && assertion.Subject.NameID != nil {
		nameID = assertion.Subject.NameID.Value
	}

	credentials := map[string]interface{}{
		"assertion_type": "saml2",
		"name_id":        nameID,
		"attributes":     attributes,
		"issued_at":      time.Now().UTC().Format(time.RFC3339),
	}

	var expiresAt *time.Time
	if assertion != nil && assertion.Conditions != nil && !assertion.Conditions.NotOnOrAfter.IsZero() {
		exp := assertion.Conditions.NotOnOrAfter
		expiresAt = &exp
		credentials["expires_at"] = exp.Format(time.RFC3339)
	} else {
		exp := time.Now().Add(time.Hour)
		expiresAt = &exp
		credentials["expires_at"] = exp.Format(time.RFC3339)
	}
	return credentials, expiresAt
}

func providerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
