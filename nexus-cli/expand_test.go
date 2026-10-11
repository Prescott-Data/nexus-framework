package main

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExpandManifest_ValuesOnlyAndMissingNamed(t *testing.T) {
	const doc = `
# A comment mentioning ${NOT_A_PLACEHOLDER} must be ignored.
providers:
  - name: github
    auth_type: oauth2
    client_id: "${GITHUB_CLIENT_ID}"
    client_secret: "${GITHUB_CLIENT_SECRET}"
    auth_url: https://github.com/login/oauth/authorize
    token_url: https://github.com/login/oauth/access_token
    scopes: ["read:user", "${EXTRA_SCOPE}"]
    params:
      validation:
        path: "/user?key=${API_PATH_KEY}"
      nested: [{inner: "${INNER}"}]
`
	var m Manifest
	if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"GITHUB_CLIENT_ID": "id", "GITHUB_CLIENT_SECRET": "sec", "EXTRA_SCOPE": "user:email", "API_PATH_KEY": "k", "INNER": "v"}
	missing := expandManifest(&m, func(n string) (string, bool) { v, ok := env[n]; return v, ok })
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none; the comment must not count", missing)
	}
	p := m.Providers[0]
	if p.ClientID != "id" || p.ClientSecret != "sec" || p.Scopes[1] != "user:email" {
		t.Fatalf("scalar fields not expanded: %+v", p)
	}
	path := p.Params["validation"].(map[string]interface{})["path"]
	inner := p.Params["nested"].([]interface{})[0].(map[string]interface{})["inner"]
	if path != "/user?key=k" || inner != "v" {
		t.Fatalf("nested params not expanded: path=%v inner=%v", path, inner)
	}

	// Unset placeholders are all reported, by name, once each.
	var m2 Manifest
	_ = yaml.Unmarshal([]byte(doc), &m2)
	missing = expandManifest(&m2, func(string) (string, bool) { return "", false })
	want := []string{"GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET", "EXTRA_SCOPE", "API_PATH_KEY", "INNER"}
	if !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing = %v, want %v", missing, want)
	}
}
