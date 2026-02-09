package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
)

func TestApplyAuthentication(t *testing.T) {
	tests := []struct {
		name        string
		strategy    AuthStrategy
		creds       Credentials
		body        []byte
		expectError bool
		validate    func(*testing.T, *http.Request)
	}{
		{
			name: "Header Auth - Default",
			strategy: AuthStrategy{
				Type: "header",
				Config: map[string]interface{}{
					"credential_field": "api_key",
				},
			},
			creds: Credentials{"api_key": "my-secret-key"},
			validate: func(t *testing.T, req *http.Request) {
				assert.Equal(t, "my-secret-key", req.Header.Get("Authorization"))
			},
		},
		{
			name: "Header Auth - Custom",
			strategy: AuthStrategy{
				Type: "header",
				Config: map[string]interface{}{
					"header_name":      "X-API-Key",
					"value_prefix":     "Token ",
					"credential_field": "token",
				},
			},
			creds: Credentials{"token": "abc-123"},
			validate: func(t *testing.T, req *http.Request) {
				assert.Equal(t, "Token abc-123", req.Header.Get("X-API-Key"))
			},
		},
		{
			name: "Query Param Auth",
			strategy: AuthStrategy{
				Type: "query_param",
				Config: map[string]interface{}{
					"param_name":       "auth_token",
					"credential_field": "key",
				},
			},
			creds: Credentials{"key": "xyz-987"},
			validate: func(t *testing.T, req *http.Request) {
				assert.Equal(t, "xyz-987", req.URL.Query().Get("auth_token"))
			},
		},
		{
			name: "Basic Auth",
			strategy: AuthStrategy{
				Type: "basic_auth",
				Config: map[string]interface{}{
					"username_field": "u",
					"password_field": "p",
				},
			},
			creds: Credentials{"u": "bob", "p": "secret"},
			validate: func(t *testing.T, req *http.Request) {
				expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("bob:secret"))
				assert.Equal(t, expected, req.Header.Get("Authorization"))
			},
		},
		{
			name: "OAuth2",
			strategy: AuthStrategy{
				Type: "oauth2",
			},
			creds: Credentials{"access_token": "oauth-token-123"},
			validate: func(t *testing.T, req *http.Request) {
				assert.Equal(t, "Bearer oauth-token-123", req.Header.Get("Authorization"))
			},
		},
		{
			name: "HMAC Payload - SHA256 Hex",
			strategy: AuthStrategy{
				Type: "hmac_payload",
				Config: map[string]interface{}{
					"header_name":  "X-Signature",
					"secret_field": "secret",
					"algo":         "sha256",
					"encoding":     "hex",
				},
			},
			creds: Credentials{"secret": "my-secret"},
			body:  []byte("payload data"),
			validate: func(t *testing.T, req *http.Request) {
				// Verify signature
				mac := hmac.New(sha256.New, []byte("my-secret"))
				mac.Write([]byte("payload data"))
				expected := hex.EncodeToString(mac.Sum(nil))
				assert.Equal(t, expected, req.Header.Get("X-Signature"))

				// Verify body is still readable
				body, err := io.ReadAll(req.Body)
				assert.NoError(t, err)
				assert.Equal(t, []byte("payload data"), body)
			},
		},
		{
			name: "HMAC Payload - Default Config (SHA256/Hex)",
			strategy: AuthStrategy{
				Type: "hmac_payload",
				Config: map[string]interface{}{
					"header_name": "X-Sig",
					// Defaults: secret_field=api_secret, algo=sha256, encoding=hex
				},
			},
			creds: Credentials{"api_secret": "default-secret"},
			body:  []byte("data"),
			validate: func(t *testing.T, req *http.Request) {
				mac := hmac.New(sha256.New, []byte("default-secret"))
				mac.Write([]byte("data"))
				expected := hex.EncodeToString(mac.Sum(nil))
				assert.Equal(t, expected, req.Header.Get("X-Sig"))
			},
		},
		{
			name: "AWS SigV4 - Success",
			strategy: AuthStrategy{
				Type: "aws_sigv4",
				Config: map[string]interface{}{
					"service": "s3",
					"region":  "us-west-2",
				},
			},
			creds: Credentials{"access_key": "AKIA...", "secret_key": "secret..."},
			body:  []byte("some-data"),
			validate: func(t *testing.T, req *http.Request) {
				authHeader := req.Header.Get("Authorization")
				assert.Contains(t, authHeader, "AWS4-HMAC-SHA256")
				assert.Contains(t, authHeader, "Credential=AKIA.../")
				assert.Contains(t, authHeader, "/us-west-2/s3/aws4_request")
				assert.Contains(t, authHeader, "Signature=")

				assert.NotEmpty(t, req.Header.Get("X-Amz-Date"))
				// Payload hash for "some-data"
				expectedHash := "9332d94d5ee69ad17d310e62cd101d70f578024fd5e8d1647f8073f886c894e1"
				assert.Equal(t, expectedHash, req.Header.Get("X-Amz-Content-Sha256"))

				// Verify body is preserved
				body, _ := io.ReadAll(req.Body)
				assert.Equal(t, []byte("some-data"), body)
			},
		},
		{
			name: "AWS SigV4 - Missing Service",
			strategy: AuthStrategy{
				Type: "aws_sigv4",
				Config: map[string]interface{}{
					"region": "us-east-1",
				},
			},
			creds:       Credentials{"access_key": "AK", "secret_key": "SK"},
			expectError: true,
		},
		{
			name: "AWS SigV4 - Missing Credentials",
			strategy: AuthStrategy{
				Type: "aws_sigv4",
				Config: map[string]interface{}{
					"service": "s3",
				},
			},
			creds:       Credentials{"access_key": "AK"}, // Missing secret key
			expectError: true,
		},
		{
			name: "Error - Basic Auth Missing Credentials",
			strategy: AuthStrategy{
				Type: "basic_auth",
			},
			creds:       Credentials{"username": "bob"}, // Missing password
			expectError: true,
		},
		{
			name: "Error - Query Auth Missing Param Name",
			strategy: AuthStrategy{
				Type: "query_param",
				Config: map[string]interface{}{
					// Missing param_name
				},
			},
			creds:       Credentials{"api_key": "123"},
			expectError: true,
		},
		{
			name: "Error - HMAC Missing Header Name",
			strategy: AuthStrategy{
				Type: "hmac_payload",
				Config: map[string]interface{}{
					// Missing header_name
				},
			},
			creds:       Credentials{"api_secret": "123"},
			expectError: true,
		},
		{
			name: "Error - Unsupported Strategy",
			strategy: AuthStrategy{
				Type: "magic_wand",
			},
			creds:       Credentials{},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			if tc.body != nil {
				req, _ = http.NewRequest("POST", "http://example.com", bytes.NewBuffer(tc.body))
			} else {
				req, _ = http.NewRequest("GET", "http://example.com", nil)
			}

			// Add a blank context for AWS SigV4 tests if needed, though NewRequest does it
			// req = req.WithContext(context.Background()) 

			err := ApplyAuthentication(req, tc.strategy, tc.creds)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, req)
				}
			}
		})
	}
}

func TestApplyGRPCAuthentication(t *testing.T) {
	tests := []struct {
		name        string
		strategy    AuthStrategy
		creds       Credentials
		expectError bool
		validate    func(*testing.T, context.Context)
	}{
		{
			name: "OAuth2",
			strategy: AuthStrategy{
				Type: "oauth2",
			},
			creds: Credentials{"access_token": "grpc-token-123"},
			validate: func(t *testing.T, ctx context.Context) {
				md, ok := metadata.FromOutgoingContext(ctx)
				assert.True(t, ok)
				assert.Equal(t, []string{"Bearer grpc-token-123"}, md["authorization"])
			},
		},
		{
			name: "Header Auth - Custom",
			strategy: AuthStrategy{
				Type: "header",
				Config: map[string]interface{}{
					"header_name":      "X-Custom-Auth",
					"credential_field": "key",
				},
			},
			creds: Credentials{"key": "secret-value"},
			validate: func(t *testing.T, ctx context.Context) {
				md, ok := metadata.FromOutgoingContext(ctx)
				assert.True(t, ok)
				// Keys must be lowercase
				assert.Equal(t, []string{"secret-value"}, md["x-custom-auth"])
			},
		},
		{
			name: "Basic Auth",
			strategy: AuthStrategy{
				Type: "basic_auth",
				Config: map[string]interface{}{
					"username_field": "u",
					"password_field": "p",
				},
			},
			creds: Credentials{"u": "user", "p": "pass"},
			validate: func(t *testing.T, ctx context.Context) {
				md, ok := metadata.FromOutgoingContext(ctx)
				assert.True(t, ok)
				expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
				assert.Equal(t, []string{expected}, md["authorization"])
			},
		},
		{
			name: "Error - Query Param Unsupported",
			strategy: AuthStrategy{
				Type: "query_param",
			},
			creds:       Credentials{"api_key": "123"},
			expectError: true,
		},
		{
			name: "Error - Missing Credentials",
			strategy: AuthStrategy{
				Type: "header",
			},
			creds:       Credentials{}, // Missing api_key
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, err := ApplyGRPCAuthentication(context.Background(), tc.strategy, tc.creds)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, ctx)
				}
			}
		})
	}
}