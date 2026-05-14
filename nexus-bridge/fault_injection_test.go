package bridge_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-bridge"
	"github.com/Prescott-Data/nexus-framework/nexus-bridge/pkg/auth"
	oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

// --- Gateway <-> Bridge Fault Injection Tests ---
// These tests verify that the Bridge correctly handles error conditions
// from the Gateway (via the SDK). If the Gateway's error contract changes,
// these tests will fail, preventing silent dependency breaks.

// mockFaultTokenProvider lets us inject specific SDK errors.
type mockFaultTokenProvider struct {
	getTokenFunc          func(ctx context.Context, connectionID string) (*auth.Token, error)
	refreshConnectionFunc func(ctx context.Context, connectionID string) (*auth.Token, error)
}

func (m *mockFaultTokenProvider) GetToken(ctx context.Context, connectionID string) (*auth.Token, error) {
	return m.getTokenFunc(ctx, connectionID)
}

func (m *mockFaultTokenProvider) RefreshConnection(ctx context.Context, connectionID string) (*auth.Token, error) {
	if m.refreshConnectionFunc != nil {
		return m.refreshConnectionFunc(ctx, connectionID)
	}
	return nil, errors.New("not implemented")
}

// TestBridge_RateLimited429_IsRecoverable verifies that when the Gateway
// returns a 429 (via ErrorEnvelope.StatusCode), the Bridge treats it as
// a recoverable error and retries instead of marking it permanent.
func TestBridge_RateLimited429_IsRecoverable(t *testing.T) {
	t.Parallel()

	var callCount int
	provider := &mockFaultTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			callCount++
			if callCount <= 2 {
				// Simulate what the SDK returns on 429
				return nil, oauthsdk.ErrorEnvelope{
					Code:       "rate_limited",
					Message:    "Too Many Requests",
					StatusCode: 429,
				}
			}
			// Third call succeeds
			return &auth.Token{
				Strategy:    auth.AuthStrategy{Type: "oauth2"},
				Credentials: auth.Credentials{"access_token": "tok"},
				ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
			}, nil
		},
	}

	retryPolicy := bridge.RetryPolicy{
		MinBackoff: 10 * time.Millisecond,
		MaxBackoff: 50 * time.Millisecond,
		Jitter:     5 * time.Millisecond,
	}
	b := bridge.New(provider, bridge.WithRetryPolicy(retryPolicy), bridge.WithLogger(&testLogger{t: t}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// MaintainWebSocket will try to get a token, get 429'd twice, then succeed
	// and try to connect to a bogus URL — which fails permanently.
	// We just need to verify the 429 was retried (callCount > 1).
	_ = b.MaintainWebSocket(ctx, "conn-rate-limited", "ws://127.0.0.1:1", &noopHandler{})

	if callCount < 2 {
		t.Errorf("expected at least 2 GetToken calls (429 should be retried), got %d", callCount)
	}
}

// TestBridge_AuthError401_IsPermanent verifies that a 401 Unauthorized
// from the Gateway is treated as a permanent error (no retry).
func TestBridge_AuthError401_IsPermanent(t *testing.T) {
	t.Parallel()

	var callCount int
	provider := &mockFaultTokenProvider{
		getTokenFunc: func(ctx context.Context, connectionID string) (*auth.Token, error) {
			callCount++
			return nil, oauthsdk.ErrorEnvelope{
				Code:       "unauthorized",
				Message:    "Invalid credentials",
				StatusCode: 401,
			}
		},
	}

	retryPolicy := bridge.RetryPolicy{
		MinBackoff: 10 * time.Millisecond,
		MaxBackoff: 50 * time.Millisecond,
		Jitter:     5 * time.Millisecond,
	}
	b := bridge.New(provider, bridge.WithRetryPolicy(retryPolicy), bridge.WithLogger(&testLogger{t: t}))

	err := b.MaintainWebSocket(context.Background(), "conn-401", "ws://127.0.0.1:1", &noopHandler{})

	// Should be a PermanentError — not retried
	var permErr *bridge.PermanentError
	if !errors.As(err, &permErr) {
		t.Fatalf("expected PermanentError for 401, got: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected exactly 1 GetToken call (401 should NOT be retried), got %d", callCount)
	}
}

// TestBridge_SDKErrorEnvelopeContract verifies the Bridge can type-assert
// on oauthsdk.ErrorEnvelope and access StatusCode. If the SDK changes the
// ErrorEnvelope type or removes StatusCode, this test breaks.
func TestBridge_SDKErrorEnvelopeContract(t *testing.T) {
	t.Parallel()

	// Create an ErrorEnvelope as the SDK would
	err := oauthsdk.ErrorEnvelope{
		Code:       "rate_limited",
		Message:    "slow down",
		StatusCode: 429,
	}

	// Verify it implements the error interface
	var _ error = err

	// Verify errors.As works (this is how the bridge detects it)
	var envErr oauthsdk.ErrorEnvelope
	if !errors.As(err, &envErr) {
		t.Fatal("errors.As failed to match ErrorEnvelope")
	}

	if envErr.StatusCode != 429 {
		t.Errorf("expected StatusCode 429, got %d", envErr.StatusCode)
	}
	if envErr.Code != "rate_limited" {
		t.Errorf("expected Code 'rate_limited', got %q", envErr.Code)
	}
}
