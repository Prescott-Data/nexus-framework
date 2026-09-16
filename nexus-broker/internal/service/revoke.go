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

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/discovery"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault"
	"github.com/google/uuid"
)

// StatusRevoked is the terminal status of a connection whose credential has
// been destroyed. Nothing transitions out of it; the user must reconnect.
const StatusRevoked = "revoked"

// maxRevocationBody bounds how much of a provider's error response we read
// before giving up, so a misbehaving endpoint cannot stream into the broker.
// The body is logged, never returned: see postRevocation.
const maxRevocationBody = 4 << 10

// providerRevocationTimeout bounds the upstream RFC 7009 call, and
// revocationCleanupTimeout bounds local teardown. They are separate budgets on
// purpose — a provider that hangs must not be able to starve the transaction
// that destroys the local credential.
const (
	providerRevocationTimeout = 10 * time.Second
	revocationCleanupTimeout  = 15 * time.Second
)

// RevokeRequest describes a revocation. WorkspaceID is optional but, when
// supplied, is enforced: a caller scoped to one workspace must not be able to
// revoke another workspace's connection by guessing a UUID.
type RevokeRequest struct {
	ConnectionID uuid.UUID
	WorkspaceID  string
	Reason       string
}

// RevokeResult reports what the revocation actually accomplished.
//
// ProviderRevoked is reported separately from the local outcome on purpose.
// Nexus always destroys its own copy of the credential, but whether the
// provider also invalidated it upstream depends on the provider supporting
// RFC 7009 and being reachable. Callers that need a hard guarantee (incident
// response) must be able to see that distinction rather than infer success.
type RevokeResult struct {
	ConnectionID    uuid.UUID `json:"connection_id"`
	ProviderName    string    `json:"provider_name"`
	Status          string    `json:"status"`
	RevokedAt       time.Time `json:"revoked_at"`
	AlreadyRevoked  bool      `json:"already_revoked"`
	TokenDeleted    bool      `json:"token_deleted"`
	SessionsClosed  int64     `json:"sessions_closed"`
	ProviderRevoked bool      `json:"provider_revoked"`
	// ProviderRevocationError explains why upstream revocation did not happen:
	// an unsupported provider, a missing endpoint, or a failed call.
	ProviderRevocationError string `json:"provider_revocation_error,omitempty"`
}

// sessionCloser closes agent sessions bound to a connection. It is the narrow
// slice of AgentRepository that revocation needs; keeping it narrow avoids a
// dependency cycle between the connection and agent services.
type sessionCloser interface {
	CloseSessionsForConnection(ctx context.Context, connectionID uuid.UUID, closedAt time.Time) (int64, error)
}

// RevokeConnection tears a connection down permanently.
//
// Order matters. The provider is called *first*, while the refresh token is
// still readable: revoking upstream after deleting our copy would be
// impossible. The local teardown then runs in a transaction and is not
// conditional on the provider call succeeding — a provider that is down must
// never leave a live credential sitting in the database.
func (s *connectionService) RevokeConnection(ctx context.Context, req RevokeRequest) (*RevokeResult, error) {
	if req.ConnectionID == uuid.Nil {
		return nil, ErrBadRequest("missing_connection_id", "connection_id is required")
	}

	conn, err := s.connRepo.GetWithProvider(ctx, req.ConnectionID)
	if err != nil {
		return nil, ErrNotFoundWithErr(err, "connection_not_found", "Connection not found")
	}

	// Report a workspace mismatch as "not found" rather than "forbidden": a 403
	// would confirm to a caller that the connection ID exists.
	if req.WorkspaceID != "" && conn.WorkspaceID != "" && req.WorkspaceID != conn.WorkspaceID {
		return nil, ErrNotFound("connection_not_found", "Connection not found")
	}

	result := &RevokeResult{
		ConnectionID: conn.ID,
		ProviderName: conn.ProviderName,
		Status:       StatusRevoked,
	}

	// Idempotent: a second revoke reports success without touching the provider
	// again. Clients retrying after a timeout must not get a 404 or a 409, and
	// the retry must describe the *original* revocation — a fabricated
	// timestamp or a reset provider_revoked would make the confirmation this
	// endpoint promises worthless.
	if conn.Status == StatusRevoked {
		s.applyPriorRevocation(ctx, result)
		return result, nil
	}

	revokedAt := time.Now().UTC()
	result.RevokedAt = revokedAt

	credentials := s.loadCredentials(ctx, conn.ID)
	if credentials != nil {
		// The upstream call gets its own deadline so an unreachable revocation
		// endpoint cannot consume the request budget that local teardown needs.
		upstreamCtx, cancel := context.WithTimeout(ctx, providerRevocationTimeout)
		result.ProviderRevoked, result.ProviderRevocationError = s.revokeUpstream(upstreamCtx, conn.ProviderID, conn.AuthType, credentials)
		cancel()
	} else {
		result.ProviderRevocationError = "no stored credential to revoke upstream"
	}

	// Local teardown runs on a context detached from cancellation: a client
	// disconnect, or a request deadline already burned by a dead provider, must
	// never abort the transaction that destroys the credential. Deleting the
	// token is the one part of revocation that has no second chance.
	cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), revocationCleanupTimeout)
	defer cancelCleanup()

	if err := s.inTx(cleanupCtx, func(txCtx context.Context) error {
		// MarkRevoked runs first so its row-exclusive lock on the connection is
		// held for the rest of the transaction. Credential writers
		// (TokenRepository.Upsert, AgentRepository.CreateSession) take a
		// FOR SHARE lock on the same row and are gated on revoked_at, so from
		// here on they either already committed — and are cleaned up below — or
		// block until this commits and are then rejected.
		rows, err := s.connRepo.MarkRevoked(txCtx, conn.ID, req.Reason, revokedAt, result.ProviderRevoked)
		if err != nil {
			return ErrInternalWithErr(err, "revoke_failed", "Failed to mark connection revoked")
		}
		// Zero rows means a concurrent revocation won: the status pre-check
		// above is outside this transaction, so two callers can both reach here.
		// The loser must not claim it performed the revocation.
		if rows == 0 {
			result.AlreadyRevoked = true
		}
		if err := s.tokenRepo.Delete(txCtx, conn.ID); err != nil {
			return ErrInternalWithErr(err, "token_delete_failed", "Failed to delete stored credential")
		}
		if s.agentRepo != nil {
			closed, err := s.agentRepo.CloseSessionsForConnection(txCtx, conn.ID, revokedAt)
			if err != nil {
				return ErrInternalWithErr(err, "session_close_failed", "Failed to close agent sessions for connection")
			}
			result.SessionsClosed = closed
		}
		return nil
	}); err != nil {
		return nil, err
	}

	result.TokenDeleted = true
	if result.AlreadyRevoked {
		// The winner's record is authoritative; report it rather than this
		// call's own outcome.
		s.applyPriorRevocation(cleanupCtx, result)
	}
	return result, nil
}

// applyPriorRevocation fills the result from the stored revocation record so a
// retry (or the loser of a concurrent revoke) confirms the original outcome.
// If the record cannot be read we fall back to "now" and say so, rather than
// silently presenting a made-up timestamp as fact.
func (s *connectionService) applyPriorRevocation(ctx context.Context, result *RevokeResult) {
	result.AlreadyRevoked = true
	result.TokenDeleted = true

	rec, err := s.connRepo.GetRevocation(ctx, result.ConnectionID)
	if err != nil || rec == nil || rec.RevokedAt == nil {
		result.RevokedAt = time.Now().UTC()
		result.ProviderRevocationError = "connection was already revoked; original revocation record unavailable"
		return
	}

	result.RevokedAt = rec.RevokedAt.UTC()
	if rec.ProviderRevoked != nil {
		result.ProviderRevoked = *rec.ProviderRevoked
	}
	if !result.ProviderRevoked {
		result.ProviderRevocationError = "connection was already revoked; the provider was not revoked upstream at that time"
	} else {
		result.ProviderRevocationError = ""
	}
}

// loadCredentials decrypts the stored credential blob, or returns nil if there
// is nothing usable. A connection with no readable token is still revocable —
// there is simply nothing to send upstream.
func (s *connectionService) loadCredentials(ctx context.Context, connectionID uuid.UUID) map[string]interface{} {
	token, err := s.tokenRepo.Get(ctx, connectionID)
	if err != nil {
		return nil
	}
	plaintext, err := vault.Decrypt(s.encryptionKey, token.EncryptedData)
	if err != nil {
		log.Printf("revoke: could not decrypt credential for connection %s: %v", connectionID, err)
		return nil
	}
	var credentials map[string]interface{}
	if err := json.Unmarshal(plaintext, &credentials); err != nil {
		return nil
	}
	return credentials
}

// revokeUpstream performs a best-effort RFC 7009 revocation. It reports whether
// the provider accepted the revocation and, if not, why not.
func (s *connectionService) revokeUpstream(ctx context.Context, providerID uuid.UUID, authType string, credentials map[string]interface{}) (bool, string) {
	// Only OAuth2 grants have an upstream revocation concept. Static API keys
	// and SAML assertions must be rotated or expired at the provider instead.
	if authType != "oauth2" && authType != "" {
		return false, fmt.Sprintf("auth type %q has no upstream revocation endpoint; the credential was destroyed locally only", authType)
	}

	p, err := s.providerStore.GetProfile(providerID)
	if err != nil {
		return false, "provider profile could not be loaded"
	}

	endpoint := s.resolveRevocationEndpoint(ctx, p.Params, p.Issuer, p.AuthURL, p.TokenURL)
	if endpoint == "" {
		return false, "provider does not advertise a revocation endpoint"
	}

	clientID, clientSecret := "", ""
	if p.ClientID != nil {
		clientID = *p.ClientID
	}
	if p.ClientSecret != nil {
		clientSecret = *p.ClientSecret
	}

	// RFC 7009 §2.1: revoking a refresh token SHOULD also invalidate the access
	// tokens derived from it, so the refresh token is the more complete request.
	// Access tokens are still revoked explicitly, because providers are allowed
	// to leave them valid until expiry and "SHOULD" is not "MUST".
	var attempted bool
	var succeeded bool
	var failures []string

	for _, hint := range []string{"refresh_token", "access_token"} {
		raw, _ := credentials[hint].(string)
		if strings.TrimSpace(raw) == "" {
			continue
		}
		attempted = true
		if err := s.postRevocation(ctx, endpoint, clientID, clientSecret, p.AuthHeader, raw, hint); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", hint, err))
			continue
		}
		succeeded = true
	}

	switch {
	case !attempted:
		return false, "no revocable OAuth token was stored for this connection"
	case len(failures) > 0 && !succeeded:
		return false, "provider rejected revocation (" + strings.Join(failures, "; ") + ")"
	case len(failures) > 0:
		return true, "partially revoked upstream (" + strings.Join(failures, "; ") + ")"
	default:
		return true, ""
	}
}

// resolveRevocationEndpoint prefers an explicitly configured endpoint over
// discovery, so an operator can always override a provider whose published
// metadata is wrong or absent.
func (s *connectionService) resolveRevocationEndpoint(ctx context.Context, params *json.RawMessage, issuer, authURL, tokenURL *string) string {
	if params != nil {
		var paramsMap map[string]interface{}
		if err := json.Unmarshal(*params, &paramsMap); err == nil {
			if endpoint, ok := paramsMap["revocation_endpoint"].(string); ok && strings.TrimSpace(endpoint) != "" {
				return strings.TrimSpace(endpoint)
			}
		}
	}

	hint := discovery.Hint{}
	if issuer != nil {
		hint.Issuer = strings.TrimSpace(*issuer)
	}
	if hint.Issuer == "" && authURL != nil {
		hint.AuthURL = strings.TrimSpace(*authURL)
	}
	if hint.Issuer == "" && hint.AuthURL == "" && tokenURL != nil {
		hint.AuthURL = strings.TrimSpace(*tokenURL)
	}
	if hint.Issuer == "" && hint.AuthURL == "" {
		return ""
	}

	md, err := discovery.Discover(ctx, s.httpClient, hint)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(md.RevocationEndpoint)
}

// postRevocation sends a single RFC 7009 revocation request.
//
// Per §2.2 a 200 means the token is no longer valid, and that includes tokens
// that were already invalid or unknown to the provider — so an already-revoked
// token is a success, not an error.
func (s *connectionService) postRevocation(ctx context.Context, endpoint, clientID, clientSecret, authHeader, token, hint string) error {
	data := url.Values{}
	data.Set("token", token)
	data.Set("token_type_hint", hint)

	useBasicAuth := strings.EqualFold(authHeader, "client_secret_basic") || strings.EqualFold(authHeader, "Basic")
	if !useBasicAuth {
		if clientID != "" {
			data.Set("client_id", clientID)
		}
		if clientSecret != "" {
			data.Set("client_secret", clientSecret)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if useBasicAuth {
		req.SetBasicAuth(clientID, clientSecret)
	}

	// Revocation is a POST, which the shared caching transport passes straight
	// through, so it reuses the same client as the token exchange and refresh
	// rather than the SSRF-guarded probe client — provider revocation endpoints
	// may legitimately live on a self-hosted, non-public host.
	resp, err := s.httpClient.Do(req)
	if err != nil {
		// Transport errors carry the endpoint URL, which may be an internal
		// host. Log the detail, report only the class.
		log.Printf("revoke: %s revocation request to %s failed: %v", hint, endpoint, err)
		return fmt.Errorf("revocation endpoint unreachable")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return nil
	}

	// Drain a bounded amount of the body so the connection can be reused, and
	// log it for operators — but never return it. A revocation endpoint is
	// free to echo the submitted token back in its error response, and this
	// error string is surfaced to the API caller in provider_revocation_error.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxRevocationBody))
	log.Printf("revoke: provider %s rejected %s revocation with status %d: %s",
		endpoint, hint, resp.StatusCode, strings.TrimSpace(string(body)))
	return fmt.Errorf("provider returned status %d", resp.StatusCode)
}
