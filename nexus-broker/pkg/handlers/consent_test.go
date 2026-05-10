package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
)

type MockConnectionService struct {
	mock.Mock
}

func (m *MockConnectionService) CreateConsentSpec(ctx context.Context, req service.CreateConsentRequest) (*service.ConsentSpecResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) != nil {
		return args.Get(0).(*service.ConsentSpecResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockConnectionService) ExchangeCodeForTokens(ctx context.Context, state, code, errorParam, errorDesc string) (string, bool, error) {
	args := m.Called(ctx, state, code, errorParam, errorDesc)
	return args.String(0), args.Bool(1), args.Error(2)
}

func (m *MockConnectionService) GetToken(ctx context.Context, connectionID uuid.UUID) (map[string]interface{}, error) {
	args := m.Called(ctx, connectionID)
	if args.Get(0) != nil {
		return args.Get(0).(map[string]interface{}), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockConnectionService) GetCaptureSchema(ctx context.Context, state string) (string, json.RawMessage, error) {
	args := m.Called(ctx, state)
	if args.Get(1) != nil {
		return args.String(0), args.Get(1).(json.RawMessage), args.Error(2)
	}
	return args.String(0), nil, args.Error(2)
}

func (m *MockConnectionService) SaveCredential(ctx context.Context, state string, credentials map[string]interface{}) (string, error) {
	args := m.Called(ctx, state, credentials)
	return args.String(0), args.Error(1)
}

func (m *MockConnectionService) Refresh(ctx context.Context, connectionID uuid.UUID) (*service.RefreshResponse, error) {
	args := m.Called(ctx, connectionID)
	if args.Get(0) != nil {
		return args.Get(0).(*service.RefreshResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func TestGetSpec_Success(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConsentHandler(ConsentHandlerConfig{
		Service: mockSvc,
	})

	reqBody := service.CreateConsentRequest{
		WorkspaceID: "ws-123",
		ProviderID:  uuid.New().String(),
		Scopes:      []string{"read"},
		ReturnURL:   "http://localhost/return",
	}

	expectedResp := &service.ConsentSpecResponse{
		AuthURL:    "http://auth.url",
		State:      "test-state",
		Scopes:     []string{"read"},
		ProviderID: reqBody.ProviderID,
	}

	mockSvc.On("CreateConsentSpec", mock.Anything, reqBody).Return(expectedResp, nil)

	jsonBody, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/auth/consent-spec", bytes.NewReader(jsonBody))
	rr := httptest.NewRecorder()

	handler.GetSpec(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	mockSvc.AssertExpectations(t)
}

func TestGetSpec_ServiceError(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConsentHandler(ConsentHandlerConfig{
		Service: mockSvc,
	})

	reqBody := service.CreateConsentRequest{
		WorkspaceID: "ws-123",
		ProviderID:  uuid.New().String(),
		Scopes:      []string{"read"},
		ReturnURL:   "http://localhost/return",
	}

	mockSvc.On("CreateConsentSpec", mock.Anything, reqBody).Return((*service.ConsentSpecResponse)(nil), service.ErrNotFound("provider_not_found", "Provider not found"))

	jsonBody, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/auth/consent-spec", bytes.NewReader(jsonBody))
	rr := httptest.NewRecorder()

	handler.GetSpec(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	mockSvc.AssertExpectations(t)
}
