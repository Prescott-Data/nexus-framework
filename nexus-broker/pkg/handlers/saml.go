package handlers

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/audit"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/auth"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/provider"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/samlutil"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/server"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type SAMLHandler struct {
	store                provider.ProfileStorer
	connRepo             repository.ConnectionRepository
	tokenRepo            repository.TokenRepository
	audit                audit.Logger
	baseURL              string
	stateKey             []byte
	encryptionKey        []byte
	enforceReturnURL     bool
	allowedReturnDomains []string
}

func NewSAMLHandler(
	store provider.ProfileStorer,
	connRepo repository.ConnectionRepository,
	tokenRepo repository.TokenRepository,
	auditSvc audit.Logger,
	baseURL string,
	stateKey, encryptionKey []byte,
	enforceReturnURL bool,
	allowedReturnDomains []string,
) *SAMLHandler {
	return &SAMLHandler{
		store:                store,
		connRepo:             connRepo,
		tokenRepo:            tokenRepo,
		audit:                auditSvc,
		baseURL:              baseURL,
		stateKey:             stateKey,
		encryptionKey:        encryptionKey,
		enforceReturnURL:     enforceReturnURL,
		allowedReturnDomains: allowedReturnDomains,
	}
}

// Metadata serves the SP metadata XML for a specific SAML provider
func (h *SAMLHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	providerIDStr := chi.URLParam(r, "providerID")
	providerID, err := uuid.Parse(providerIDStr)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_provider_id", "Invalid provider ID")
		return
	}

	p, err := h.store.GetProfile(providerID)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "provider_not_found", "Provider not found")
		return
	}

	if p.AuthType != "saml" {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_auth_type", "Provider is not a SAML provider")
		return
	}

	acsURL, _ := url.JoinPath(h.baseURL, "/saml/acs")
	sp, err := samlutil.BuildServiceProvider(*p.SAMLSPEntityID, acsURL, *p.SAMLIdpEntityID, *p.SAMLIdpSSOURL, *p.SAMLIdpX509Cert)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "saml_sp_build_failed", "Failed to build SAML SP")
		return
	}

	metadata := sp.Metadata()
	b, err := xml.MarshalIndent(metadata, "", "  ")
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "metadata_marshal_failed", "Failed to marshal metadata")
		return
	}

	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	w.Write([]byte(xml.Header))
	w.Write(b)
}

// ACS handles the SAML POST binding from the IdP
func (h *SAMLHandler) ACS(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_request", "Failed to parse form")
		return
	}

	relayState := r.FormValue("RelayState")
	if relayState == "" {
		httputil.WriteError(w, http.StatusBadRequest, "missing_relay_state", "Missing RelayState")
		return
	}

	stateData, err := auth.VerifyState(h.stateKey, relayState)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_state", "Invalid state")
		return
	}

	connID, err := uuid.Parse(stateData.Nonce)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_connection_id", "Invalid connection ID in state")
		return
	}

	conn, err := h.connRepo.GetPending(r.Context(), connID)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "connection_not_found", "Connection not found or expired")
		return
	}

	p, err := h.store.GetProfile(conn.ProviderID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "provider_not_found", "Provider not found")
		return
	}

	acsURL, _ := url.JoinPath(h.baseURL, "/saml/acs")
	sp, err := samlutil.BuildServiceProvider(*p.SAMLSPEntityID, acsURL, *p.SAMLIdpEntityID, *p.SAMLIdpSSOURL, *p.SAMLIdpX509Cert)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "saml_sp_build_failed", "Failed to build SAML SP")
		return
	}

	assertion, err := sp.ParseResponse(r, []string{""})
	if err != nil {
		h.connRepo.UpdateStatus(r.Context(), connID, "failed")
		httputil.WriteError(w, http.StatusUnauthorized, "saml_verification_failed", fmt.Sprintf("SAML validation failed: %v", err))
		return
	}

	attributes := make(map[string]interface{})
	for _, attr := range assertion.AttributeStatements {
		for _, a := range attr.Attributes {
			if len(a.Values) == 1 {
				attributes[a.Name] = a.Values[0].Value
			} else if len(a.Values) > 1 {
				var vals []string
				for _, v := range a.Values {
					vals = append(vals, v.Value)
				}
				attributes[a.Name] = vals
			}
		}
	}
	
	nameID := ""
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		nameID = assertion.Subject.NameID.Value
	}
	attributes["name_id"] = nameID

	tokenData := map[string]interface{}{
		"access_token": "saml-assertion",
		"attributes":   attributes,
	}

	tokenJSON, err := json.Marshal(tokenData)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "token_marshal_failed", "Failed to serialize token")
		return
	}

	encryptedData, err := vault.Encrypt(h.encryptionKey, tokenJSON)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "encryption_failed", "Failed to encrypt tokens")
		return
	}

	var expiresAt *time.Time
	if assertion.Conditions != nil && !assertion.Conditions.NotOnOrAfter.IsZero() {
		t := assertion.Conditions.NotOnOrAfter
		expiresAt = &t
	} else {
		exp := time.Now().Add(1 * time.Hour)
		expiresAt = &exp
	}

	if err := h.inTx(r.Context(), func(txCtx context.Context) error {
		if err := h.tokenRepo.Upsert(txCtx, &domain.Token{
			ConnectionID:  connID,
			EncryptedData: encryptedData,
			ExpiresAt:     expiresAt,
		}); err != nil {
			return err
		}
		if err := h.connRepo.UpdateStatus(txCtx, connID, "active"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "database_error", "Failed to finalize connection")
		return
	}

	if !server.IsReturnURLAllowed(conn.ReturnURL, h.enforceReturnURL, h.allowedReturnDomains) {
		httputil.WriteError(w, http.StatusBadRequest, "return_url_not_allowed", "return_url not allowed")
		return
	}

	returnURL, err := url.Parse(conn.ReturnURL)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "invalid_return_url", "Invalid return_url")
		return
	}

	query := returnURL.Query()
	query.Set("status", "success")
	query.Set("connection_id", connID.String())
	query.Set("provider", p.Name)
	returnURL.RawQuery = query.Encode()

	http.Redirect(w, r, returnURL.String(), http.StatusFound)
}

func (h *SAMLHandler) inTx(ctx context.Context, fn func(context.Context) error) error {
	type txRunner interface {
		InTx(ctx context.Context, fn func(context.Context) error) error
	}
	if runner, ok := h.connRepo.(txRunner); ok {
		return runner.InTx(ctx, fn)
	}
	return fn(ctx)
}
