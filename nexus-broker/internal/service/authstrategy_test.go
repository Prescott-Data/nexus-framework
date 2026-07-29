package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func rawParams(t *testing.T, v map[string]interface{}) *json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	rm := json.RawMessage(b)
	return &rm
}

func TestResolveAuthStrategy_LegacyApiKeyBearer(t *testing.T) {
	s := resolveAuthStrategy("api_key", "", nil)
	if s.Type != "header" || s.Config["value_prefix"] != "Bearer " || s.Config["header_name"] != "Authorization" {
		t.Fatalf("unexpected legacy bearer strategy: %+v", s)
	}
}

func TestResolveAuthStrategy_LegacyApiKeyCustomHeader(t *testing.T) {
	s := resolveAuthStrategy("api_key", "X-API-Key", nil)
	if s.Type != "header" || s.Config["header_name"] != "X-API-Key" {
		t.Fatalf("unexpected custom header strategy: %+v", s)
	}
	if p, ok := s.Config["value_prefix"]; ok && p != "" {
		t.Fatalf("custom header must not carry a prefix, got %v", p)
	}
}

func TestResolveAuthStrategy_FromParamsOverrides(t *testing.T) {
	params := rawParams(t, map[string]interface{}{
		"auth_strategy": map[string]interface{}{
			"type":   "query_param",
			"config": map[string]interface{}{"param_name": "key", "credential_field": "api_key"},
		},
	})
	s := resolveAuthStrategy("api_key", "", params)
	if s.Type != "query_param" || s.Config["param_name"] != "key" {
		t.Fatalf("params.auth_strategy should win: %+v", s)
	}
}

func TestApplyAuthStrategy_HeaderWithPrefix(t *testing.T) {
	// travis-ci style: "Authorization: token <key>"
	req, _ := http.NewRequest("GET", "https://example.com/user", nil)
	strat := authStrategy{Type: "header", Config: map[string]interface{}{
		"header_name": "Authorization", "value_prefix": "token ", "credential_field": "api_key",
	}}
	if err := applyAuthStrategy(req, strat, map[string]interface{}{"api_key": "abc"}); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "token abc" {
		t.Fatalf("want 'token abc', got %q", got)
	}
}

func TestApplyAuthStrategy_QueryParam(t *testing.T) {
	// trello style: ?key=<key>
	req, _ := http.NewRequest("GET", "https://api.trello.com/1/members/me", nil)
	strat := authStrategy{Type: "query_param", Config: map[string]interface{}{"param_name": "key"}}
	if err := applyAuthStrategy(req, strat, map[string]interface{}{"api_key": "xyz"}); err != nil {
		t.Fatal(err)
	}
	if got := req.URL.Query().Get("key"); got != "xyz" {
		t.Fatalf("want query key=xyz, got %q", got)
	}
}

func TestApplyAuthStrategy_Basic(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	strat := authStrategy{Type: "basic_auth"}
	if err := applyAuthStrategy(req, strat, map[string]interface{}{"username": "u", "password": "p"}); err != nil {
		t.Fatal(err)
	}
	u, p, ok := req.BasicAuth()
	if !ok || u != "u" || p != "p" {
		t.Fatalf("basic auth not applied: %v %v %v", u, p, ok)
	}
}

func TestApplyAuthStrategy_MissingCredential(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	strat := authStrategy{Type: "header", Config: map[string]interface{}{"credential_field": "api_key"}}
	if err := applyAuthStrategy(req, strat, map[string]interface{}{}); err == nil {
		t.Fatal("expected missing_credential error")
	}
}

func TestApplyAuthStrategy_UnsupportedIsSentinel(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	strat := authStrategy{Type: "aws_sigv4"}
	if err := applyAuthStrategy(req, strat, map[string]interface{}{}); err != errUnsupportedValidation {
		t.Fatalf("want errUnsupportedValidation, got %v", err)
	}
}

func mkResp(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestEvaluateValidation_Default401Rejected(t *testing.T) {
	if err := evaluateValidation(mkResp(401, ""), validationRule{}); err == nil {
		t.Fatal("401 should be rejected")
	}
	if err := evaluateValidation(mkResp(200, ""), validationRule{}); err != nil {
		t.Fatalf("200 should pass, got %v", err)
	}
}

func TestEvaluateValidation_SlackBodyFailure(t *testing.T) {
	// Slack returns 200 with {"ok":false} for a bad token.
	rule := validationRule{FailureBodyContains: `"ok":false`}
	if err := evaluateValidation(mkResp(200, `{"ok":false,"error":"invalid_auth"}`), rule); err == nil {
		t.Fatal("slack invalid_auth body should be rejected despite 200")
	}
	if err := evaluateValidation(mkResp(200, `{"ok":true}`), rule); err != nil {
		t.Fatalf("slack ok:true should pass, got %v", err)
	}
}

func TestEvaluateValidation_SuccessBodyRequired(t *testing.T) {
	rule := validationRule{SuccessBodyContains: "account_id"}
	if err := evaluateValidation(mkResp(200, `{"account_id":"1"}`), rule); err != nil {
		t.Fatalf("should pass when marker present, got %v", err)
	}
	if err := evaluateValidation(mkResp(200, `{"nope":true}`), rule); err == nil {
		t.Fatal("should reject when success marker absent")
	}
}

// End-to-end sanity: a query-param provider validates against a live test server
// that only accepts the key in the query string (the exact Group B failure mode).
func TestValidateCredentials_QueryParamProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") == "good" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	svc := &connectionService{httpClient: srv.Client()}
	params := rawParams(t, map[string]interface{}{
		"auth_strategy": map[string]interface{}{
			"type":   "query_param",
			"config": map[string]interface{}{"param_name": "key"},
		},
	})

	// Good key → accepted (previously failed because the probe forced a header).
	if err := svc.validateCredentials(context.Background(), "api_key", "", srv.URL, "/me", params, map[string]interface{}{"api_key": "good"}); err != nil {
		t.Fatalf("valid query-param key should pass, got %v", err)
	}
	// Bad key → rejected.
	if err := svc.validateCredentials(context.Background(), "api_key", "", srv.URL, "/me", params, map[string]interface{}{"api_key": "bad"}); err == nil {
		t.Fatal("bad query-param key should be rejected")
	}
}
