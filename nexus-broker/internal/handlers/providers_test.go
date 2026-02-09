package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"dromos.com/nexus-broker/internal/provider"
)

// MockStore is a mock implementation of the provider.ProfileStorer interface.
type MockStore struct {
	mock.Mock
}

func (m *MockStore) RegisterProfile(profileJSON string) (*provider.Profile, error) {
	args := m.Called(profileJSON)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*provider.Profile), args.Error(1)
}

func (m *MockStore) GetProfile(id uuid.UUID) (*provider.Profile, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*provider.Profile), args.Error(1)
}

func (m *MockStore) GetProfileByName(name string) (*provider.Profile, error) {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*provider.Profile), args.Error(1)
}

func (m *MockStore) UpdateProfile(p *provider.Profile) error {
	args := m.Called(p)
	return args.Error(0)
}

func (m *MockStore) PatchProfile(id uuid.UUID, updates map[string]interface{}) error {
	// Mock implementation for testing
	return nil
}

func (m *MockStore) DeleteProfile(id uuid.UUID) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockStore) DeleteProfileByName(name string) (int64, error) {
	args := m.Called(name)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockStore) ListProfiles() ([]provider.ProfileList, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]provider.ProfileList), args.Error(1)
}

func (m *MockStore) GetMetadata() (map[string]map[string]interface{}, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]map[string]interface{}), args.Error(1)
}

func TestRegisterProvider_Success(t *testing.T) {
	// 1. Mocks the provider.Store.
	mockStore := new(MockStore)
	handler := NewProvidersHandler(mockStore)

	// 2. Mocks the store.RegisterProfile method to return a valid Profile.
	expectedProfile := &provider.Profile{
		ID:           uuid.New(),
		Name:         "Test OAuth2 Provider",
		AuthType:     "oauth2",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		AuthURL:      "http://provider.com/auth",
		TokenURL:     "http://provider.com/token",
		Scopes:       []string{"openid"},
	}
	mockStore.On("RegisterProfile", mock.AnythingOfType("string")).Return(expectedProfile, nil)

	// 3. Sends a POST request with a valid JSON payload.
	profileData := map[string]interface{}{
		"name":          "Test OAuth2 Provider",
		"auth_type":     "oauth2",
		"client_id":     "test-client-id",
		"client_secret": "test-client-secret",
		"auth_url":      "http://provider.com/auth",
		"token_url":     "http://provider.com/token",
		"scopes":        []string{"openid"},
	}
	body := map[string]interface{}{
		"profile": profileData,
	}
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", "/providers", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.Register(rr, req)

	// 4. Asserts that the store.RegisterProfile method was called exactly once with the raw JSON payload.
	expectedProfileJSON, _ := json.Marshal(profileData)
	mockStore.AssertCalled(t, "RegisterProfile", string(expectedProfileJSON))
	mockStore.AssertNumberOfCalls(t, "RegisterProfile", 1)

	// 5. Asserts the response is http.StatusCreated.
	assert.Equal(t, http.StatusCreated, rr.Code)

	// 6. Asserts the response body matches the expected profile.
	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, expectedProfile.ID.String(), response["id"])
}

func TestRegisterProvider_StoreError(t *testing.T) {
	// 1. Mocks the provider.Store.
	mockStore := new(MockStore)
	handler := NewProvidersHandler(mockStore)

	// 2. Mocks the store.RegisterProfile method to return an error.
	expectedError := errors.New("validation failed")
	mockStore.On("RegisterProfile", mock.AnythingOfType("string")).Return(nil, expectedError)

	// 3. Sends a POST request.
	body := map[string]interface{}{"profile": map[string]interface{}{"name": "Test"}}
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", "/providers", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.Register(rr, req)

	// 4. Asserts that the store.RegisterProfile method was called.
	mockStore.AssertCalled(t, "RegisterProfile", mock.AnythingOfType("string"))

	// 5. Asserts the response is http.StatusBadRequest and contains the error message.
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), expectedError.Error())
}

func TestRegisterProvider_InvalidJSON(t *testing.T) {
	// 1. Mocks the provider.Store.
	mockStore := new(MockStore)
	handler := NewProvidersHandler(mockStore)

	// 2. Sends a POST request with invalid JSON.
	req, err := http.NewRequest("POST", "/providers", bytes.NewReader([]byte("invalid json")))
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.Register(rr, req)

	// 3. Asserts that the store.RegisterProfile method was NOT called.
	mockStore.AssertNotCalled(t, "RegisterProfile", mock.Anything)

	// 4. Asserts the response is http.StatusBadRequest.
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}
