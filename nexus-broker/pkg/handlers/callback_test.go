package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/go-chi/chi/v5"
)

func TestHandleCallback_Success(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewCallbackHandler(CallbackHandlerConfig{
		Service: mockSvc,
	})

	mockSvc.On("ExchangeCodeForTokens", mock.Anything, "test-state", "test-code", "", "").
		Return("http://localhost/return?status=success", true, nil)

	req, _ := http.NewRequest("GET", "/auth/callback?state=test-state&code=test-code", nil)
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)

	assert.Equal(t, http.StatusFound, rr.Code)
	assert.Equal(t, "http://localhost/return?status=success", rr.Header().Get("Location"))
	mockSvc.AssertExpectations(t)
}

func TestHandleCallback_Error(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewCallbackHandler(CallbackHandlerConfig{
		Service: mockSvc,
	})

	mockSvc.On("ExchangeCodeForTokens", mock.Anything, "test-state", "test-code", "", "").
		Return("", false, errors.New("exchange failed"))

	req, _ := http.NewRequest("GET", "/auth/callback?state=test-state&code=test-code", nil)
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	mockSvc.AssertExpectations(t)
}

func TestGetToken_Success(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewCallbackHandler(CallbackHandlerConfig{
		Service: mockSvc,
	})

	connID := uuid.New()
	expectedToken := map[string]interface{}{
		"credentials": map[string]interface{}{"access_token": "abc"},
		"strategy":    map[string]interface{}{"type": "oauth2"},
	}

	mockSvc.On("GetToken", mock.Anything, connID).Return(expectedToken, nil)

	req, _ := http.NewRequest("GET", "/connections/"+connID.String()+"/token", nil)
	
	// Add chi route context
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("connectionID", connID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	handler.GetToken(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	mockSvc.AssertExpectations(t)
}

func TestRefresh_Success(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewCallbackHandler(CallbackHandlerConfig{
		Service: mockSvc,
	})

	connID := uuid.New()
	expectedResp := &service.RefreshResponse{
		Tokens:     map[string]interface{}{"access_token": "new-token"},
		StatusCode: 200,
	}

	mockSvc.On("Refresh", mock.Anything, connID).Return(expectedResp, nil)

	req, _ := http.NewRequest("POST", "/connections/"+connID.String()+"/refresh", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("connectionID", connID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	handler.Refresh(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	mockSvc.AssertExpectations(t)
}

func TestGetCaptureSchema_Success(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewCallbackHandler(CallbackHandlerConfig{
		Service: mockSvc,
	})

	mockSchema := json.RawMessage(`{"type":"object"}`)
	mockSvc.On("GetCaptureSchema", mock.Anything, "test-state").Return("TestProvider", mockSchema, nil)

	req, _ := http.NewRequest("GET", "/auth/capture-schema?state=test-state", nil)
	rr := httptest.NewRecorder()

	handler.GetCaptureSchema(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	mockSvc.AssertExpectations(t)
}

func TestSaveCredential_Success(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewCallbackHandler(CallbackHandlerConfig{
		Service: mockSvc,
	})

	creds := map[string]interface{}{"api_key": "123"}
	mockSvc.On("SaveCredential", mock.Anything, "test-state", creds).Return("http://localhost/return", nil)

	body, _ := json.Marshal(map[string]interface{}{
		"state":       "test-state",
		"credentials": creds,
	})

	req, _ := http.NewRequest("POST", "/auth/capture-credential", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.SaveCredential(rr, req)

	assert.Equal(t, http.StatusFound, rr.Code)
	mockSvc.AssertExpectations(t)
}
