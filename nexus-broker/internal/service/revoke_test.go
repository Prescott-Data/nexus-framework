package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/provider"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault"
)

// MockSessionCloser records the agent-session cascade triggered by revocation.
type MockSessionCloser struct {
	mock.Mock
}

func (m *MockSessionCloser) CloseSessionsForConnection(ctx context.Context, connectionID uuid.UUID, closedAt time.Time) (int64, error) {
	args := m.Called(ctx, connectionID, closedAt)
	return args.Get(0).(int64), args.Error(1)
}

var revokeTestEncryptionKey = []byte("12345678901234567890123456789012")

func encryptCreds(t *testing.T, creds map[string]interface{}) string {
	t.Helper()
	raw, err := json.Marshal(creds)
	require.NoError(t, err)
	enc, err := vault.Encrypt(revokeTestEncryptionKey, raw)
	require.NoError(t, err)
	return enc
}

func setupRevokeService(t *testing.T, closer *MockSessionCloser) (*MockConnectionRepository, *MockTokenRepository, *MockProfileStorer, service.ConnectionService) {
	t.Helper()
	connRepo := new(MockConnectionRepository)
	tokenRepo := new(MockTokenRepository)
	providerStore := new(MockProfileStorer)

	var opts []service.ConnectionServiceOption
	if closer != nil {
		opts = append(opts, service.WithAgentSessionCloser(closer))
	}

	svc := service.NewConnectionService(
		connRepo,
		tokenRepo,
		providerStore,
		nil,
		"http://localhost:8080",
		"/auth/callback",
		revokeTestEncryptionKey,
		revokeTestEncryptionKey,
		http.DefaultClient,
		false,
		[]string{},
		opts...,
	)
	return connRepo, tokenRepo, providerStore, svc
}

// A successful revocation must call the provider's RFC 7009 endpoint, destroy
// the local token, mark the connection revoked and close outstanding sessions.
func TestRevokeConnection_OAuth2_RevokesUpstreamAndLocally(t *testing.T) {
	closer := new(MockSessionCloser)
	connRepo, tokenRepo, providerStore, svc := setupRevokeService(t, closer)

	connID := uuid.New()
	providerID := uuid.New()

	var revoked []url.Values
	revocationSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		revoked = append(revoked, r.PostForm)
		w.WriteHeader(http.StatusOK)
	}))
	defer revocationSrv.Close()

	params := json.RawMessage(`{"revocation_endpoint":"` + revocationSrv.URL + `"}`)
	connRepo.On("GetWithProvider", mock.Anything, connID).Return(&domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:          connID,
			WorkspaceID: "ws-1",
			ProviderID:  providerID,
			Status:      "active",
		},
		ProviderName:   "google",
		AuthType:       "oauth2",
		ProviderParams: &params,
	}, nil).Once()

	providerStore.On("GetProfile", providerID).Return(&provider.Profile{
		ID:           providerID,
		Name:         "google",
		AuthType:     "oauth2",
		ClientID:     ptr("client-abc"),
		ClientSecret: ptr("secret-xyz"),
		Params:       &params,
	}, nil).Once()

	tokenRepo.On("Get", mock.Anything, connID).Return(&domain.Token{
		ConnectionID:  connID,
		EncryptedData: encryptCreds(t, map[string]interface{}{"access_token": "at-1", "refresh_token": "rt-1"}),
	}, nil).Once()

	tokenRepo.On("Delete", mock.Anything, connID).Return(nil).Once()
	connRepo.On("MarkRevoked", mock.Anything, connID, "user disconnected", mock.Anything).Return(nil).Once()
	closer.On("CloseSessionsForConnection", mock.Anything, connID, mock.Anything).Return(int64(3), nil).Once()

	result, err := svc.RevokeConnection(context.Background(), service.RevokeRequest{
		ConnectionID: connID,
		WorkspaceID:  "ws-1",
		Reason:       "user disconnected",
	})

	require.NoError(t, err)
	assert.Equal(t, service.StatusRevoked, result.Status)
	assert.True(t, result.TokenDeleted)
	assert.True(t, result.ProviderRevoked)
	assert.Empty(t, result.ProviderRevocationError)
	assert.Equal(t, int64(3), result.SessionsClosed)

	// Both the refresh token and the access token are revoked, each with the
	// correct hint, and the client credentials are presented to the provider.
	require.Len(t, revoked, 2)
	assert.Equal(t, "rt-1", revoked[0].Get("token"))
	assert.Equal(t, "refresh_token", revoked[0].Get("token_type_hint"))
	assert.Equal(t, "client-abc", revoked[0].Get("client_id"))
	assert.Equal(t, "secret-xyz", revoked[0].Get("client_secret"))
	assert.Equal(t, "at-1", revoked[1].Get("token"))
	assert.Equal(t, "access_token", revoked[1].Get("token_type_hint"))

	connRepo.AssertExpectations(t)
	tokenRepo.AssertExpectations(t)
	closer.AssertExpectations(t)
}

// The whole point of the guarantee: if the provider is down or rejects the
// call, Nexus must still destroy its own copy of the credential.
func TestRevokeConnection_ProviderFailure_StillDeletesLocalToken(t *testing.T) {
	connRepo, tokenRepo, providerStore, svc := setupRevokeService(t, nil)

	connID := uuid.New()
	providerID := uuid.New()

	revocationSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer revocationSrv.Close()

	params := json.RawMessage(`{"revocation_endpoint":"` + revocationSrv.URL + `"}`)
	connRepo.On("GetWithProvider", mock.Anything, connID).Return(&domain.ConnectionWithProvider{
		Connection:     domain.Connection{ID: connID, WorkspaceID: "ws-1", ProviderID: providerID, Status: "active"},
		ProviderName:   "google",
		AuthType:       "oauth2",
		ProviderParams: &params,
	}, nil).Once()

	providerStore.On("GetProfile", providerID).Return(&provider.Profile{
		ID: providerID, AuthType: "oauth2", ClientID: ptr("c"), ClientSecret: ptr("s"), Params: &params,
	}, nil).Once()

	tokenRepo.On("Get", mock.Anything, connID).Return(&domain.Token{
		ConnectionID:  connID,
		EncryptedData: encryptCreds(t, map[string]interface{}{"refresh_token": "rt-1"}),
	}, nil).Once()
	tokenRepo.On("Delete", mock.Anything, connID).Return(nil).Once()
	connRepo.On("MarkRevoked", mock.Anything, connID, "", mock.Anything).Return(nil).Once()

	result, err := svc.RevokeConnection(context.Background(), service.RevokeRequest{ConnectionID: connID})

	require.NoError(t, err)
	assert.True(t, result.TokenDeleted)
	assert.False(t, result.ProviderRevoked)
	assert.Contains(t, result.ProviderRevocationError, "provider rejected revocation")
	tokenRepo.AssertExpectations(t)
	connRepo.AssertExpectations(t)
}

// A caller scoped to one workspace must not be able to revoke another
// workspace's connection, and must not learn that the ID exists.
func TestRevokeConnection_WorkspaceMismatchIsNotFound(t *testing.T) {
	connRepo, tokenRepo, _, svc := setupRevokeService(t, nil)

	connID := uuid.New()
	connRepo.On("GetWithProvider", mock.Anything, connID).Return(&domain.ConnectionWithProvider{
		Connection:   domain.Connection{ID: connID, WorkspaceID: "ws-owner", Status: "active"},
		ProviderName: "google",
		AuthType:     "oauth2",
	}, nil).Once()

	_, err := svc.RevokeConnection(context.Background(), service.RevokeRequest{
		ConnectionID: connID,
		WorkspaceID:  "ws-attacker",
	})

	require.Error(t, err)
	var svcErr *service.ServiceError
	require.ErrorAs(t, err, &svcErr)
	assert.Equal(t, http.StatusNotFound, svcErr.HTTPStatus)
	tokenRepo.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything)
}

// Revoking twice must succeed rather than error, so a client retrying after a
// timeout can confirm the outcome.
func TestRevokeConnection_AlreadyRevokedIsIdempotent(t *testing.T) {
	connRepo, tokenRepo, _, svc := setupRevokeService(t, nil)

	connID := uuid.New()
	connRepo.On("GetWithProvider", mock.Anything, connID).Return(&domain.ConnectionWithProvider{
		Connection:   domain.Connection{ID: connID, WorkspaceID: "ws-1", Status: service.StatusRevoked},
		ProviderName: "google",
		AuthType:     "oauth2",
	}, nil).Once()

	result, err := svc.RevokeConnection(context.Background(), service.RevokeRequest{ConnectionID: connID})

	require.NoError(t, err)
	assert.True(t, result.AlreadyRevoked)
	assert.Equal(t, service.StatusRevoked, result.Status)
	tokenRepo.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything)
	connRepo.AssertNotCalled(t, "MarkRevoked", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// Static credentials have no upstream revocation endpoint, but must still be
// destroyed locally with an explicit explanation of what was not done.
func TestRevokeConnection_APIKeyDestroysLocallyOnly(t *testing.T) {
	connRepo, tokenRepo, _, svc := setupRevokeService(t, nil)

	connID := uuid.New()
	connRepo.On("GetWithProvider", mock.Anything, connID).Return(&domain.ConnectionWithProvider{
		Connection:   domain.Connection{ID: connID, WorkspaceID: "ws-1", Status: "active"},
		ProviderName: "stripe",
		AuthType:     "api_key",
	}, nil).Once()

	tokenRepo.On("Get", mock.Anything, connID).Return(&domain.Token{
		ConnectionID:  connID,
		EncryptedData: encryptCreds(t, map[string]interface{}{"api_key": "sk-live-123"}),
	}, nil).Once()
	tokenRepo.On("Delete", mock.Anything, connID).Return(nil).Once()
	connRepo.On("MarkRevoked", mock.Anything, connID, "", mock.Anything).Return(nil).Once()

	result, err := svc.RevokeConnection(context.Background(), service.RevokeRequest{ConnectionID: connID})

	require.NoError(t, err)
	assert.True(t, result.TokenDeleted)
	assert.False(t, result.ProviderRevoked)
	assert.Contains(t, result.ProviderRevocationError, "no upstream revocation endpoint")
	tokenRepo.AssertExpectations(t)
}

// A revoked connection must never hand out a token again.
func TestGetToken_RevokedConnectionIsGone(t *testing.T) {
	connRepo, _, _, svc := setupRevokeService(t, nil)

	connID := uuid.New()
	connRepo.On("GetWithProvider", mock.Anything, connID).Return(&domain.ConnectionWithProvider{
		Connection:   domain.Connection{ID: connID, Status: service.StatusRevoked},
		ProviderName: "google",
		AuthType:     "oauth2",
	}, nil).Once()

	_, _, err := svc.GetToken(context.Background(), connID)

	require.Error(t, err)
	var svcErr *service.ServiceError
	require.ErrorAs(t, err, &svcErr)
	assert.Equal(t, http.StatusGone, svcErr.HTTPStatus)
	assert.Equal(t, "connection_revoked", svcErr.Code)
}

func TestRevokeConnection_MissingConnectionID(t *testing.T) {
	_, _, _, svc := setupRevokeService(t, nil)

	_, err := svc.RevokeConnection(context.Background(), service.RevokeRequest{})

	require.Error(t, err)
	var svcErr *service.ServiceError
	require.ErrorAs(t, err, &svcErr)
	assert.Equal(t, http.StatusBadRequest, svcErr.HTTPStatus)
}
