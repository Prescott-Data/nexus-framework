package cli

import (
	"reflect"
	"testing"
)

const expandDoc = `
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
      skip_scope_on_exchange: ${SKIP}
      retries: ${RETRIES}
      label: "${SKIP}"
      tagged: !!str ${SKIP}
      validation:
        path: "/user?key=${API_PATH_KEY}"
      nested: [{inner: "${INNER}"}]
`

func TestLoadManifest_ExpandsValuesKeepsTypesSkipsComments(t *testing.T) {
	env := map[string]string{
		"GITHUB_CLIENT_ID": "id", "GITHUB_CLIENT_SECRET": "sec", "EXTRA_SCOPE": "user:email",
		"SKIP": "true", "RETRIES": "3", "API_PATH_KEY": "k", "INNER": "v",
	}
	m, missing, err := loadManifest([]byte(expandDoc), func(n string) (string, bool) { v, ok := env[n]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none; the comment must not count", missing)
	}
	p := m.Providers[0]
	if p.ClientID != "id" || p.ClientSecret != "sec" || p.Scopes[1] != "user:email" {
		t.Fatalf("scalar fields not expanded: %+v", p)
	}
	// A bare placeholder is re-typed as the raw-text expansion used to; a quoted one stays a string.
	if v, ok := p.Params["skip_scope_on_exchange"].(bool); !ok || !v {
		t.Fatalf("skip_scope_on_exchange = %#v, want bool true", p.Params["skip_scope_on_exchange"])
	}
	if v, ok := p.Params["retries"].(int); !ok || v != 3 {
		t.Fatalf("retries = %#v, want int 3", p.Params["retries"])
	}
	if v, ok := p.Params["label"].(string); !ok || v != "true" {
		t.Fatalf("label = %#v, want string \"true\"", p.Params["label"])
	}
	if v, ok := p.Params["tagged"].(string); !ok || v != "true" {
		t.Fatalf("tagged = %#v, want the explicit !!str to be kept", p.Params["tagged"])
	}
	path := p.Params["validation"].(map[string]interface{})["path"]
	inner := p.Params["nested"].([]interface{})[0].(map[string]interface{})["inner"]
	if path != "/user?key=k" || inner != "v" {
		t.Fatalf("nested params not expanded: path=%v inner=%v", path, inner)
	}
}

func TestLoadManifest_ReportsMissingInDocumentOrder(t *testing.T) {
	for i := 0; i < 20; i++ { // would flake if the walk went through a Go map
		_, missing, err := loadManifest([]byte(expandDoc), func(string) (string, bool) { return "", false })
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET", "EXTRA_SCOPE", "SKIP", "RETRIES", "API_PATH_KEY", "INNER"}
		if !reflect.DeepEqual(missing, want) {
			t.Fatalf("missing = %v, want %v", missing, want)
		}
	}
}

func TestComputeDrift_RedactedSecretIsNotDrift(t *testing.T) {
	desired := Provider{Name: "github", AuthType: "oauth2", ClientID: "id", ClientSecret: "sec",
		AuthURL: "https://a", TokenURL: "https://t", Scopes: []string{"read:user"}}
	live := map[string]interface{}{"name": "github", "auth_type": "oauth2", "client_id": "id",
		"auth_url": "https://a", "token_url": "https://t", "scopes": []interface{}{"read:user"}, "enable_discovery": false}
	drifted, updates, err := computeDrift(desired, live)
	if err != nil {
		t.Fatal(err)
	}
	if drifted {
		t.Fatalf("a secret the Broker does not return, with no fingerprint, must not count as drift; updates = %v", updates)
	}
	live["client_id"] = "other"
	if drifted, _, _ = computeDrift(desired, live); !drifted {
		t.Fatal("a real difference must still be drift")
	}
	live["client_id"] = "id"

	// With a fingerprint the secret is compared: same secret, no drift.
	live["client_secret_fingerprint"] = secretFingerprint("sec")
	if drifted, updates, _ = computeDrift(desired, live); drifted {
		t.Fatalf("matching fingerprint must not be drift; updates = %v", updates)
	}
	// A rotated secret in the manifest is detected and sent, nothing else.
	desired.ClientSecret = "sec-2"
	drifted, updates, _ = computeDrift(desired, live)
	if !drifted || updates["client_secret"] != "sec-2" || len(updates) != 1 {
		t.Fatalf("changed secret must be the only update; drifted=%v updates=%v", drifted, updates)
	}
}
