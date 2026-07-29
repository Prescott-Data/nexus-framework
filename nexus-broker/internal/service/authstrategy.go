package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// authStrategy describes how a credential is applied to an HTTP request. It
// mirrors the runtime strategy used by nexus-bridge (pkg/auth) so that
// connect-time validation and background health checks authenticate a provider
// exactly the way real requests will. The broker and bridge are separate Go
// modules (and the broker's Docker build context excludes the bridge), so this
// is a deliberate, minimal in-module mirror rather than a shared import.
type authStrategy struct {
	Type   string                 `json:"type"`
	Config map[string]interface{} `json:"config"`
}

// validationRule describes how to interpret a validation response body/status.
// It is read from the provider's params under the "validation" key and lets us
// handle providers that signal auth failure with a 200 + error body (e.g. Slack
// returns 200 with {"ok":false}).
type validationRule struct {
	// Skip marks the provider as explicitly non-validatable (e.g. write-only
	// ingestion keys like Segment/Heap where no endpoint can verify the key).
	// The credential is stored without a probe instead of failing closed.
	Skip bool `json:"skip"`
	// FailureStatus overrides the default {401,403} "rejected" status codes.
	FailureStatus []int `json:"failure_status"`
	// FailureBodyContains: if the response body contains this substring, the
	// credential is treated as rejected regardless of status code.
	FailureBodyContains string `json:"failure_body_contains"`
	// SuccessBodyContains: if set, the response body MUST contain this substring
	// for the credential to be considered valid.
	SuccessBodyContains string `json:"success_body_contains"`
}

// resolveAuthStrategy determines the auth strategy for a provider. It prefers an
// explicit params.auth_strategy (the same config the bridge uses at runtime);
// otherwise it falls back to the legacy behavior for api_key / basic_auth so
// existing providers keep working unchanged.
func resolveAuthStrategy(authType, authHeader string, providerParams *json.RawMessage) authStrategy {
	if providerParams != nil {
		var params struct {
			AuthStrategy *authStrategy `json:"auth_strategy"`
		}
		if err := json.Unmarshal(*providerParams, &params); err == nil && params.AuthStrategy != nil && params.AuthStrategy.Type != "" {
			return *params.AuthStrategy
		}
	}

	switch authType {
	case "api_key":
		// Legacy default: "Authorization: Bearer <key>" unless a custom header
		// is configured, in which case the raw key is set on that header.
		if authHeader == "" || strings.EqualFold(authHeader, "Authorization") {
			return authStrategy{Type: "header", Config: map[string]interface{}{
				"header_name":      "Authorization",
				"value_prefix":     "Bearer ",
				"credential_field": "api_key",
			}}
		}
		return authStrategy{Type: "header", Config: map[string]interface{}{
			"header_name":      authHeader,
			"credential_field": "api_key",
		}}
	case "basic_auth":
		return authStrategy{Type: "basic_auth", Config: map[string]interface{}{
			"username_field": "username",
			"password_field": "password",
		}}
	default:
		return authStrategy{Type: authType}
	}
}

// errUnsupportedValidation signals that the broker cannot build a validation
// request for the given strategy at connect time (e.g. body-signing schemes).
// Callers decide whether to skip validation (best-effort) or fail closed.
var errUnsupportedValidation = fmt.Errorf("auth strategy not supported for connect-time validation")

// applyAuthStrategy injects credentials into req according to strat. It mirrors
// nexus-bridge's ApplyAuthentication for the strategies that can be exercised
// with a simple GET validation request.
func applyAuthStrategy(req *http.Request, strat authStrategy, creds map[string]interface{}) error {
	switch strat.Type {
	case "header", "oauth2":
		return applyHeaderStrategy(req, strat, creds)
	case "query_param":
		return applyQueryStrategy(req, strat.Config, creds)
	case "basic_auth":
		return applyBasicStrategy(req, strat.Config, creds)
	case "path":
		// The credential is carried in the URL path via a {field} template that
		// renderEndpoint substitutes before the request is built (e.g. Telegram's
		// /bot{api_key}/getMe). Nothing to inject into headers/query here.
		return nil
	default:
		// hmac_payload / aws_sigv4 require request-body signing that a generic
		// validation probe cannot meaningfully perform.
		return errUnsupportedValidation
	}
}

// templatePlaceholder matches {field} tokens in an endpoint path.
var templatePlaceholder = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

// renderEndpoint substitutes {field} placeholders in an endpoint/path with the
// corresponding (path-escaped) credential values. Placeholders whose field is
// not present in creds are left untouched. This enables path-based auth such as
// Telegram (/bot{api_key}/getMe) and WhatsApp (/{phone_number_id}/...).
func renderEndpoint(endpoint string, creds map[string]interface{}) string {
	if !strings.Contains(endpoint, "{") {
		return endpoint
	}
	return templatePlaceholder.ReplaceAllStringFunc(endpoint, func(tok string) string {
		field := tok[1 : len(tok)-1]
		if v, ok := creds[field].(string); ok && v != "" {
			return url.PathEscape(v)
		}
		return tok
	})
}

// effectiveBaseURL returns the base URL to use for a connection. Self-hosted /
// instance-specific providers have no global api_base_url; instead the user
// supplies their instance URL at connect time, stored as "base_url" in the
// per-connection credential blob. The provider's configured base URL always
// wins when present.
func effectiveBaseURL(providerBaseURL string, creds map[string]interface{}) string {
	if strings.TrimSpace(providerBaseURL) != "" {
		return providerBaseURL
	}
	if v, ok := creds["base_url"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// isValidBaseURL reports whether a user-supplied base URL is a well-formed
// absolute http(s) URL. Self-hosted providers may legitimately point at
// internal hosts, so we only enforce scheme and host presence.
func isValidBaseURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func strFromConfig(config map[string]interface{}, key string) string {
	if config == nil {
		return ""
	}
	s, _ := config[key].(string)
	return s
}

func credString(creds map[string]interface{}, field string) (string, error) {
	val, ok := creds[field]
	if !ok || val == nil {
		return "", ErrBadRequest("missing_credential", fmt.Sprintf("The '%s' credential is required", field))
	}
	s, ok := val.(string)
	if !ok || s == "" {
		return "", ErrBadRequest("missing_credential", fmt.Sprintf("The '%s' credential is required", field))
	}
	return s, nil
}

func applyHeaderStrategy(req *http.Request, strat authStrategy, creds map[string]interface{}) error {
	config := strat.Config
	headerName := strFromConfig(config, "header_name")
	if headerName == "" {
		headerName = "Authorization"
	}
	credField := strFromConfig(config, "credential_field")
	if credField == "" {
		if strat.Type == "oauth2" {
			credField = "access_token"
		} else {
			credField = "api_key"
		}
	}
	prefix := strFromConfig(config, "value_prefix")
	if prefix == "" && strat.Type == "oauth2" {
		prefix = "Bearer "
	}
	val, err := credString(creds, credField)
	if err != nil {
		return err
	}
	req.Header.Set(headerName, prefix+val)
	return nil
}

func applyQueryStrategy(req *http.Request, config map[string]interface{}, creds map[string]interface{}) error {
	paramName := strFromConfig(config, "param_name")
	if paramName == "" {
		return ErrBadRequest("invalid_auth_strategy", "query_param strategy requires 'param_name'")
	}
	credField := strFromConfig(config, "credential_field")
	if credField == "" {
		credField = "api_key"
	}
	val, err := credString(creds, credField)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	q.Add(paramName, val)
	req.URL.RawQuery = q.Encode()
	return nil
}

func applyBasicStrategy(req *http.Request, config map[string]interface{}, creds map[string]interface{}) error {
	userField := strFromConfig(config, "username_field")
	if userField == "" {
		userField = "username"
	}
	passField := strFromConfig(config, "password_field")
	if passField == "" {
		passField = "password"
	}
	username, _ := creds[userField].(string)
	password, _ := creds[passField].(string)
	if username == "" || password == "" {
		return ErrBadRequest("missing_credential", fmt.Sprintf("The '%s' and '%s' credentials are required", userField, passField))
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	req.Header.Set("Authorization", "Basic "+encoded)
	return nil
}

// parseValidationRule extracts the optional validation rule from provider params.
func parseValidationRule(providerParams *json.RawMessage) validationRule {
	var rule validationRule
	if providerParams == nil {
		return rule
	}
	var params struct {
		Validation *validationRule `json:"validation"`
	}
	if err := json.Unmarshal(*providerParams, &params); err == nil && params.Validation != nil {
		rule = *params.Validation
	}
	return rule
}

// evaluateValidation decides whether a validation response indicates the
// credential is valid. It returns nil when valid, or a "credentials_rejected"
// error when the provider rejected the credential.
func evaluateValidation(resp *http.Response, rule validationRule) error {
	failureStatuses := rule.FailureStatus
	if len(failureStatuses) == 0 {
		failureStatuses = []int{http.StatusUnauthorized, http.StatusForbidden}
	}
	for _, code := range failureStatuses {
		if resp.StatusCode == code {
			return ErrBadRequest("credentials_rejected", "The provider rejected these credentials")
		}
	}

	// Body-aware checks (e.g. Slack returns 200 with {"ok":false} for a bad token).
	if rule.FailureBodyContains != "" || rule.SuccessBodyContains != "" {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		text := string(body)
		if rule.FailureBodyContains != "" && strings.Contains(text, rule.FailureBodyContains) {
			return ErrBadRequest("credentials_rejected", "The provider rejected these credentials")
		}
		if rule.SuccessBodyContains != "" && !strings.Contains(text, rule.SuccessBodyContains) {
			return ErrBadRequest("credentials_rejected", "The provider rejected these credentials")
		}
	}
	return nil
}
