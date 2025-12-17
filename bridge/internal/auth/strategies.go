package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"google.golang.org/grpc/metadata"
)

// Credentials is a type alias for a map of credential values.
type Credentials map[string]interface{}

// AuthStrategy represents the configuration for an authentication method.
type AuthStrategy struct {
	Type   string                 `json:"type"`
	Config map[string]interface{} `json:"config"`
}

// applyHeaderAuth injects authentication credentials into the request header.
func applyHeaderAuth(req *http.Request, config map[string]interface{}, creds Credentials) error {
	// 1. Read config["header_name"]. Default to "Authorization" if nil/empty.
	headerName, _ := config["header_name"].(string)
	if headerName == "" {
		headerName = "Authorization"
	}

	// 2. Read config["credential_field"]. Default to "api_key" if nil/empty.
	credField, _ := config["credential_field"].(string)
	if credField == "" {
		credField = "api_key"
	}

	// 3. Look up the value in creds. Return an error if missing or empty.
	val, ok := creds[credField]
	if !ok || val == nil {
		return fmt.Errorf("credential field '%s' is missing", credField)
	}

	valStr, ok := val.(string)
	if !ok || valStr == "" {
		return fmt.Errorf("credential field '%s' is empty or not a string", credField)
	}

	// 4. Check for config["value_prefix"] (e.g., "Bearer "). If present, prepend it to the value.
	if prefix, ok := config["value_prefix"].(string); ok && prefix != "" {
		valStr = prefix + valStr
	}

	// 5. Set the header on the request using req.Header.Set().
	req.Header.Set(headerName, valStr)

	return nil
}

// applyQueryAuth injects authentication credentials into the request query parameters.
func applyQueryAuth(req *http.Request, config map[string]interface{}, creds Credentials) error {
	// 1. Read config["param_name"]. Return an error if this is missing or empty (it is required).
	paramName, _ := config["param_name"].(string)
	if paramName == "" {
		return fmt.Errorf("config 'param_name' is required for query auth strategy")
	}

	// 2. Read config["credential_field"]. Default to "api_key" if nil/empty.
	credField, _ := config["credential_field"].(string)
	if credField == "" {
		credField = "api_key"
	}

	// 3. Look up the value in creds. Return an error if missing or empty.
	val, ok := creds[credField]
	if !ok || val == nil {
		return fmt.Errorf("credential field '%s' is missing", credField)
	}

	valStr, ok := val.(string)
	if !ok || valStr == "" {
		return fmt.Errorf("credential field '%s' is empty or not a string", credField)
	}

	// 4. Safely append the parameter to the request.
	q := req.URL.Query()
	q.Add(paramName, valStr)
	req.URL.RawQuery = q.Encode()

	return nil
}

// applyBasicAuth injects authentication credentials using HTTP Basic Auth.
func applyBasicAuth(req *http.Request, config map[string]interface{}, creds Credentials) error {
	// 1. Read config["username_field"]. Default to "username" if nil/empty.
	userField, _ := config["username_field"].(string)
	if userField == "" {
		userField = "username"
	}

	// 2. Read config["password_field"]. Default to "password" if nil/empty.
	passField, _ := config["password_field"].(string)
	if passField == "" {
		passField = "password"
	}

	// 3. Look up both values in creds. Return an error if either the username or password values are missing or empty.
	userVal, ok := creds[userField]
	if !ok || userVal == nil {
		return fmt.Errorf("credential field '%s' (username) is missing", userField)
	}
	username, ok := userVal.(string)
	if !ok || username == "" {
		return fmt.Errorf("credential field '%s' (username) is empty or not a string", userField)
	}

	passVal, ok := creds[passField]
	if !ok || passVal == nil {
		return fmt.Errorf("credential field '%s' (password) is missing", passField)
	}
	password, ok := passVal.(string)
	if !ok || password == "" {
		return fmt.Errorf("credential field '%s' (password) is empty or not a string", passField)
	}

	// 4. Use the standard library method req.SetBasicAuth(username, password) to apply the header.
	req.SetBasicAuth(username, password)

	return nil
}

// applyHMACPayload signs the request body and injects the signature into the header.
func applyHMACPayload(req *http.Request, config map[string]interface{}, creds Credentials) error {
	// 1. Configuration
	headerName, _ := config["header_name"].(string)
	if headerName == "" {
		return fmt.Errorf("config 'header_name' is required for hmac_payload strategy")
	}

	secretField, _ := config["secret_field"].(string)
	if secretField == "" {
		secretField = "api_secret"
	}

	algo, _ := config["algo"].(string)
	if algo == "" {
		algo = "sha256"
	}

	encoding, _ := config["encoding"].(string)
	if encoding == "" {
		encoding = "hex"
	}

	// 2. Retrieve Secret
	secretVal, ok := creds[secretField]
	if !ok || secretVal == nil {
		return fmt.Errorf("credential field '%s' is missing", secretField)
	}
	secretStr, ok := secretVal.(string)
	if !ok || secretStr == "" {
		return fmt.Errorf("credential field '%s' is empty or not a string", secretField)
	}

	// 3. Body Handling
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return fmt.Errorf("failed to read request body: %w", err)
		}
		// Restore the body so it can be read again
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	} else {
		bodyBytes = []byte{}
	}

	// 4. Calculation
	var h hash.Hash
	switch algo {
	case "sha256":
		h = hmac.New(sha256.New, []byte(secretStr))
	case "sha1":
		h = hmac.New(sha1.New, []byte(secretStr))
	default:
		return fmt.Errorf("unsupported hmac algorithm: %s", algo)
	}

	h.Write(bodyBytes)
	signatureBytes := h.Sum(nil)

	// 5. Encoding & Output
	var signature string
	switch encoding {
	case "hex":
		signature = hex.EncodeToString(signatureBytes)
	case "base64":
		signature = base64.StdEncoding.EncodeToString(signatureBytes)
	default:
		return fmt.Errorf("unsupported encoding: %s", encoding)
	}

	req.Header.Set(headerName, signature)

	return nil
}

// applyAWSSigV4 signs the request using AWS Signature Version 4.
func applyAWSSigV4(req *http.Request, config map[string]interface{}, creds Credentials) error {
	// 1. Extract Config
	region, _ := config["region"].(string)
	if region == "" {
		region = "us-east-1"
	}

	service, _ := config["service"].(string)
	if service == "" {
		return fmt.Errorf("config 'service' is required for aws_sigv4 strategy")
	}

	// 2. Extract Credentials
	accessKeyIDVal, ok := creds["access_key"]
	if !ok || accessKeyIDVal == nil {
		// Try alternate name if needed, but default is "access_key" per instructions
		// Could check "access_key_id" too if we wanted to be flexible
		return fmt.Errorf("credential field 'access_key' is missing")
	}
	accessKeyID, ok := accessKeyIDVal.(string)
	if !ok || accessKeyID == "" {
		return fmt.Errorf("credential field 'access_key' is empty or not a string")
	}

	secretAccessKeyVal, ok := creds["secret_key"]
	if !ok || secretAccessKeyVal == nil {
		return fmt.Errorf("credential field 'secret_key' is missing")
	}
	secretAccessKey, ok := secretAccessKeyVal.(string)
	if !ok || secretAccessKey == "" {
		return fmt.Errorf("credential field 'secret_key' is empty or not a string")
	}
    
    // Optional: Session Token
    sessionToken := ""
    if val, ok := creds["session_token"].(string); ok {
        sessionToken = val
    }

	// 3. Prepare Payload Hash
	var payloadHash string
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return fmt.Errorf("failed to read request body: %w", err)
		}
		// Restore the body
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	} else {
		bodyBytes = []byte{}
	}

	hash := sha256.Sum256(bodyBytes)
	payloadHash = hex.EncodeToString(hash[:])
	
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	// 4. Sign the Request
	credentials := aws.Credentials{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
        SessionToken:    sessionToken,
	}

	signer := v4.NewSigner()
	// SignHTTP(ctx, credentials, request, payloadHash, service, region, time)
	err := signer.SignHTTP(context.Background(), credentials, req, payloadHash, service, region, time.Now())
	if err != nil {
		return fmt.Errorf("failed to sign request with AWS SigV4: %w", err)
	}

	return nil
}

// ApplyAuthentication applies the authentication strategy to the request.
func ApplyAuthentication(req *http.Request, strategy AuthStrategy, creds Credentials) error {
	switch strategy.Type {
	case "header":
		return applyHeaderAuth(req, strategy.Config, creds)
	case "query_param":
		return applyQueryAuth(req, strategy.Config, creds)
	case "basic_auth":
		return applyBasicAuth(req, strategy.Config, creds)
	case "hmac_payload":
		return applyHMACPayload(req, strategy.Config, creds)
	case "aws_sigv4":
		return applyAWSSigV4(req, strategy.Config, creds)
	case "oauth2":
		// OAuth2 is just a specific configuration of Header auth
		oauthConfig := map[string]interface{}{
			"header_name":      "Authorization",
			"value_prefix":     "Bearer ",
			"credential_field": "access_token",
		}
		return applyHeaderAuth(req, oauthConfig, creds)
	default:
		return fmt.Errorf("unsupported auth strategy type: %s", strategy.Type)
	}
}

// GetGRPCMetadata generates the metadata map for gRPC authentication.

func GetGRPCMetadata(strategy AuthStrategy, creds Credentials) (map[string]string, error) {

	md := make(map[string]string)



	switch strategy.Type {

	case "header", "oauth2":

		// Determine the key (default "authorization")

		key := "authorization" // Default

		if strategy.Type == "header" {

			if k, ok := strategy.Config["header_name"].(string); ok && k != "" {

				key = strings.ToLower(k) // gRPC metadata keys must be lowercase

			}

		}



		// Determine credential field

		credField := "access_token" // Default for oauth2

		if strategy.Type == "header" {

			credField = "api_key" // Default for header

			if k, ok := strategy.Config["credential_field"].(string); ok && k != "" {

				credField = k

			}

		}



		// Get value

		val, ok := creds[credField]

		if !ok || val == nil {

			return nil, fmt.Errorf("credential field '%s' is missing", credField)

		}

		valStr, ok := val.(string)

		if !ok {

			return nil, fmt.Errorf("credential field '%s' is not a string", credField)

		}



		// Prefix

		prefix := "Bearer " // Default for oauth2

		if strategy.Type == "header" {

			prefix = "" // Default for header

			if k, ok := strategy.Config["value_prefix"].(string); ok && k != "" {

				prefix = k

			}

		}



		md[key] = prefix + valStr



	case "basic_auth":

		// Config fields

		userField := "username"

		if k, ok := strategy.Config["username_field"].(string); ok && k != "" {

			userField = k

		}

		passField := "password"

		if k, ok := strategy.Config["password_field"].(string); ok && k != "" {

			passField = k

		}



		// Get credentials

		uVal, ok := creds[userField]

		if !ok {

			return nil, fmt.Errorf("credential field '%s' is missing", userField)

		}

		uStr, ok := uVal.(string)

		if !ok {

			return nil, fmt.Errorf("credential field '%s' is not a string", userField)

		}



		pVal, ok := creds[passField]

		if !ok {

			return nil, fmt.Errorf("credential field '%s' is missing", passField)

		}

		pStr, ok := pVal.(string)

		if !ok {

			return nil, fmt.Errorf("credential field '%s' is not a string", passField)

		}



		// Encode Basic Auth

		auth := base64.StdEncoding.EncodeToString([]byte(uStr + ":" + pStr))

		md["authorization"] = "Basic " + auth



	case "query_param":

		return nil, fmt.Errorf("query_param authentication is not supported for gRPC")



	default:

		return nil, fmt.Errorf("unsupported auth strategy type for gRPC: %s", strategy.Type)

	}



	return md, nil

}



// ApplyGRPCAuthentication injects authentication credentials into the context metadata for gRPC calls.

func ApplyGRPCAuthentication(ctx context.Context, strategy AuthStrategy, creds Credentials) (context.Context, error) {

	md, err := GetGRPCMetadata(strategy, creds)

	if err != nil {

		return nil, err

	}

	// Create new outgoing context with metadata

	return metadata.NewOutgoingContext(ctx, metadata.New(md)), nil

}
