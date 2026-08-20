package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
)

func TestSAMLMetadata_Success(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewSAMLHandler(mockSvc)
	providerID := uuid.New()
	metadata := []byte(`<?xml version="1.0"?><EntityDescriptor/>`)

	req := httptest.NewRequest(http.MethodGet, "/saml/metadata/"+providerID.String(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("providerID", providerID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	mockSvc.On("GetSAMLMetadata", mock.Anything, providerID).Return(metadata, nil)

	handler.Metadata(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/samlmetadata+xml", w.Header().Get("Content-Type"))
	assert.Equal(t, string(metadata), w.Body.String())
	mockSvc.AssertExpectations(t)
}

func TestSAMLMetadata_InvalidProviderID(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewSAMLHandler(mockSvc)

	req := httptest.NewRequest(http.MethodGet, "/saml/metadata/not-a-uuid", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("providerID", "not-a-uuid")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.Metadata(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_provider_id")
	mockSvc.AssertNotCalled(t, "GetSAMLMetadata", mock.Anything, mock.Anything)
}

func TestSAMLACS_Success(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewSAMLHandler(mockSvc)
	returnURL := "http://app.example.com/callback?status=success&connection_id=abc"

	req := httptest.NewRequest(http.MethodPost, "/saml/acs", strings.NewReader("SAMLResponse=abc&RelayState=state"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	mockSvc.On("ExchangeSAMLResponse", mock.Anything, req).Return(returnURL, nil)

	handler.ACS(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, returnURL, w.Header().Get("Location"))
	mockSvc.AssertExpectations(t)
}

func TestSAMLACS_ServiceError(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewSAMLHandler(mockSvc)
	req := httptest.NewRequest(http.MethodPost, "/saml/acs", strings.NewReader("RelayState=state"))
	w := httptest.NewRecorder()

	mockSvc.On("ExchangeSAMLResponse", mock.Anything, req).Return("", service.ErrBadRequest("missing_saml_response", "Missing SAMLResponse"))

	handler.ACS(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing_saml_response")
	mockSvc.AssertExpectations(t)
}
