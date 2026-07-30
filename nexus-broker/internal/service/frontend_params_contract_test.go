package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// =============================================================================
// Frontend → broker params contract
// =============================================================================
//
// The provider-registration UI (doc-intel-dashboard,
// app/app-launcher/connected-apps/provider-info) serialises provider config into
// params.auth_strategy / params.validation / params.credential_schema. These
// tests pin the exact JSON that form emits and drive it through the real
// validation path, so a change on either side that breaks the contract fails
// here rather than silently mis-authenticating a provider in production.
//
// Each fixture below is the verbatim output of buildStaticParams() for the
// corresponding provider in nexus-cli/nexus-providers-static.yaml.
// =============================================================================

func feParams(t *testing.T, raw string) *json.RawMessage {
	t.Helper()
	if !json.Valid([]byte(raw)) {
		t.Fatalf("fixture is not valid JSON: %s", raw)
	}
	rm := json.RawMessage(raw)
	return &rm
}

// recorder captures what actually went out on the wire.
type recorder struct {
	authHeader string
	path       string
	rawQuery   string
}

func newProbeServer(t *testing.T, rec *recorder, respond func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.authHeader = r.Header.Get("Authorization")
		rec.path = r.URL.Path
		rec.rawQuery = r.URL.RawQuery
		respond(w, r)
	}))
}

// trello: two credentials, both carried in a query template on the endpoint.
func TestFEContract_PathTemplateWithQuery_Trello(t *testing.T) {
	rec := &recorder{}
	srv := newProbeServer(t, rec, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("key") == "k_good" && q.Get("token") == "t_good" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	defer srv.Close()

	params := feParams(t, `{
      "credential_schema": {"type":"object","required":["api_key","api_token"],
        "properties":{
          "api_key":{"type":"string","title":"API Key","format":"password"},
          "api_token":{"type":"string","title":"API Token","format":"password"}}},
      "auth_strategy": {"type":"path"}
    }`)

	endpoint := "/1/members/me?key={api_key}&token={api_token}"
	svc := &connectionService{httpClient: srv.Client()}
	if err := svc.validateCredentials(context.Background(), "api_key", "", srv.URL, endpoint, params,
		map[string]interface{}{"api_key": "k_good", "api_token": "t_good"}); err != nil {
		t.Fatalf("good key+token should validate: %v", err)
	}
	if err := svc.validateCredentials(context.Background(), "api_key", "", srv.URL, endpoint, params,
		map[string]interface{}{"api_key": "k_good", "api_token": "t_bad"}); err == nil {
		t.Fatal("bad token must be rejected")
	}
}

// slack-bot: returns HTTP 200 with {"ok":false} for a bad token. Without the
// body rule the broker would treat that as success and falsely validate.
func TestFEContract_BodyAwareValidation_SlackBot(t *testing.T) {
	rec := &recorder{}
	srv := newProbeServer(t, rec, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if r.Header.Get("Authorization") == "Bearer xoxb-good" {
			_, _ = w.Write([]byte(`{"ok":true,"user":"bot"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	})
	defer srv.Close()

	params := feParams(t, `{
      "credential_schema": {"type":"object","required":["api_key"],
        "properties":{"api_key":{"type":"string","title":"Bot Token (xoxb-...)","format":"password"}}},
      "auth_strategy": {"type":"header","config":{
        "header_name":"Authorization","value_prefix":"Bearer ","credential_field":"api_key"}},
      "validation": {"failure_body_contains":"\"ok\":false"}
    }`)

	svc := &connectionService{httpClient: srv.Client()}
	if err := svc.validateCredentials(context.Background(), "api_key", "", srv.URL, "/auth.test", params,
		map[string]interface{}{"api_key": "xoxb-good"}); err != nil {
		t.Fatalf("good token should validate: %v", err)
	}
	err := svc.validateCredentials(context.Background(), "api_key", "", srv.URL, "/auth.test", params,
		map[string]interface{}{"api_key": "xoxb-bad"})
	if err == nil {
		t.Fatal("200 + {\"ok\":false} must be treated as a rejection")
	}
}

// jenkins: self-hosted. No provider api_base_url — the instance URL arrives as
// the user's base_url credential, and the pair is sent as HTTP Basic.
func TestFEContract_SelfHostedBasic_Jenkins(t *testing.T) {
	rec := &recorder{}
	srv := newProbeServer(t, rec, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if ok && user == "admin" && pass == "token_good" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	defer srv.Close()

	params := feParams(t, `{
      "credential_schema": {"type":"object","required":["base_url","username","password"],
        "properties":{
          "base_url":{"type":"string","title":"Instance URL","format":"uri"},
          "username":{"type":"string","title":"Username"},
          "password":{"type":"string","title":"API Token","format":"password"}}},
      "auth_strategy": {"type":"basic_auth","config":{
        "username_field":"username","password_field":"password"}}
    }`)

	svc := &connectionService{httpClient: srv.Client()}
	good := map[string]interface{}{"base_url": srv.URL, "username": "admin", "password": "token_good"}
	if err := svc.validateCredentials(context.Background(), "basic_auth", "", "", "/me/api/json", params, good); err != nil {
		t.Fatalf("self-hosted basic auth should validate: %v", err)
	}
	if rec.path != "/me/api/json" {
		t.Fatalf("probe hit unexpected path %q", rec.path)
	}
	bad := map[string]interface{}{"base_url": srv.URL, "username": "admin", "password": "nope"}
	if err := svc.validateCredentials(context.Background(), "basic_auth", "", "", "/me/api/json", params, bad); err == nil {
		t.Fatal("bad password must be rejected")
	}
}

// twilio: Basic auth over non-default field names. This is the limitation the
// 2026-07-09 audit flagged — the validator used to require literal
// username/password, forcing providers to rename natural fields.
func TestFEContract_BasicCustomFieldNames_Twilio(t *testing.T) {
	rec := &recorder{}
	srv := newProbeServer(t, rec, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if ok && user == "AC_sid" && pass == "auth_good" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	defer srv.Close()

	params := feParams(t, `{
      "credential_schema": {"type":"object","required":["account_sid","auth_token"],
        "properties":{
          "account_sid":{"type":"string","title":"Account SID"},
          "auth_token":{"type":"string","title":"Auth Token","format":"password"}}},
      "auth_strategy": {"type":"basic_auth","config":{
        "username_field":"account_sid","password_field":"auth_token"}}
    }`)

	svc := &connectionService{httpClient: srv.Client()}
	good := map[string]interface{}{"account_sid": "AC_sid", "auth_token": "auth_good"}
	if err := svc.validateCredentials(context.Background(), "basic_auth", "", srv.URL, "/2010-04-01/Accounts.json", params, good); err != nil {
		t.Fatalf("custom-named basic fields should validate: %v", err)
	}
	bad := map[string]interface{}{"account_sid": "AC_sid", "auth_token": "wrong"}
	if err := svc.validateCredentials(context.Background(), "basic_auth", "", srv.URL, "/2010-04-01/Accounts.json", params, bad); err == nil {
		t.Fatal("bad auth token must be rejected")
	}
}

// infobip: self-hosted base URL *and* a custom header prefix together.
func TestFEContract_SelfHostedWithHeaderPrefix_Infobip(t *testing.T) {
	rec := &recorder{}
	srv := newProbeServer(t, rec, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "App key_good" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	defer srv.Close()

	params := feParams(t, `{
      "credential_schema": {"type":"object","required":["base_url","api_key"],
        "properties":{
          "base_url":{"type":"string","title":"Instance URL","format":"uri"},
          "api_key":{"type":"string","title":"API Key","format":"password"}}},
      "auth_strategy": {"type":"header","config":{
        "header_name":"Authorization","value_prefix":"App ","credential_field":"api_key"}}
    }`)

	svc := &connectionService{httpClient: srv.Client()}
	good := map[string]interface{}{"base_url": srv.URL, "api_key": "key_good"}
	if err := svc.validateCredentials(context.Background(), "api_key", "", "", "/account/1/balance", params, good); err != nil {
		t.Fatalf("self-hosted + prefixed header should validate: %v", err)
	}
	if rec.authHeader != "App key_good" {
		t.Fatalf("wire header mismatch: %q", rec.authHeader)
	}
}

// A blank prefix must mean "send the raw key", not "default to Bearer" — the UI
// states this explicitly, so the two sides must agree.
func TestFEContract_EmptyPrefixSendsRawKey(t *testing.T) {
	rec := &recorder{}
	srv := newProbeServer(t, rec, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	params := feParams(t, `{
      "auth_strategy": {"type":"header","config":{
        "header_name":"Authorization","value_prefix":"","credential_field":"api_key"}}
    }`)

	svc := &connectionService{httpClient: srv.Client()}
	if err := svc.validateCredentials(context.Background(), "api_key", "", srv.URL, "/me", params,
		map[string]interface{}{"api_key": "raw123"}); err != nil {
		t.Fatal(err)
	}
	if rec.authHeader != "raw123" {
		t.Fatalf("empty prefix should send the raw key, got %q", rec.authHeader)
	}
}
