package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeBroker is an in-memory Broker: a provider list with full profiles,
// recording every mutation it receives.
type fakeBroker struct {
	mu        sync.Mutex
	providers map[string]map[string]interface{} // id → profile
	mutations []string                          // "METHOD path body"
	agents    map[string]bool                   // User-Agent values seen
	apiKeys   map[string]bool
	failPost  int // status to return from POST, 0 for 201
}

func newFakeBroker() *fakeBroker {
	return &fakeBroker{providers: map[string]map[string]interface{}{}, agents: map[string]bool{}, apiKeys: map[string]bool{}}
}

func (b *fakeBroker) add(id, name string, extra map[string]interface{}) {
	p := map[string]interface{}{"id": id, "name": name, "auth_type": "oauth2", "enable_discovery": false, "scopes": []interface{}{"a"}}
	for k, v := range extra {
		p[k] = v
	}
	b.providers[id] = p
}

func (b *fakeBroker) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.agents[r.Header.Get("User-Agent")] = true
		b.apiKeys[r.Header.Get("X-API-Key")] = true
		var body bytes.Buffer
		body.ReadFrom(r.Body)
		switch {
		case r.Method == "GET" && r.URL.Path == "/providers":
			var list []map[string]interface{}
			for id, p := range b.providers {
				list = append(list, map[string]interface{}{"id": id, "name": p["name"]})
			}
			json.NewEncoder(w).Encode(list)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/providers/"):
			p, ok := b.providers[strings.TrimPrefix(r.URL.Path, "/providers/")]
			if !ok {
				http.Error(w, "not found", 404)
				return
			}
			json.NewEncoder(w).Encode(p)
		case r.Method == "POST" && r.URL.Path == "/providers":
			b.mutations = append(b.mutations, "POST /providers "+body.String())
			if b.failPost != 0 {
				http.Error(w, `{"error":"invalid_profile","message":"client_secret: missing required field"}`, b.failPost)
				return
			}
			w.WriteHeader(201)
		case r.Method == "PATCH":
			b.mutations = append(b.mutations, "PATCH "+r.URL.Path+" "+body.String())
		case r.Method == "DELETE":
			b.mutations = append(b.mutations, "DELETE "+r.URL.Path)
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 500)
		}
	})
}

const planManifest = `providers:
  - name: github
    auth_type: oauth2
    client_id: ${GH_ID}
    client_secret: ${GH_SECRET}
    auth_url: https://github.com/login/oauth/authorize
    token_url: https://github.com/login/oauth/access_token
    scopes: [a]
  - name: salesforce
    auth_type: oauth2
    client_id: sf-id
    client_secret: sf-secret
    auth_url: https://login.salesforce.com/services/oauth2/authorize
    token_url: https://login.salesforce.com/services/oauth2/token
    scopes: [a, b]
  - name: brand-new
    auth_type: api_key
  - name: hooks
    auth_type: api_key
    params:
      webhook_secret: ${WH_SECRET}
`

// setupBroker starts a fake Broker holding github (matching), salesforce
// (drifted scopes, matching secret fingerprint) and an orphan, points the
// CLI at it, and returns it with the manifest path.
func setupBroker(t *testing.T) (*fakeBroker, string) {
	t.Helper()
	b := newFakeBroker()
	b.add("id-gh", "github", map[string]interface{}{
		"client_id": "gh-id", "client_secret_fingerprint": secretFingerprint("gh-secret"),
		"auth_url": "https://github.com/login/oauth/authorize", "token_url": "https://github.com/login/oauth/access_token",
	})
	b.add("id-sf", "salesforce", map[string]interface{}{
		"client_id": "sf-id", "client_secret_fingerprint": secretFingerprint("sf-secret"),
		"auth_url": "https://login.salesforce.com/services/oauth2/authorize", "token_url": "https://login.salesforce.com/services/oauth2/token",
	})
	b.add("id-old", "old-provider", nil)
	b.add("id-hooks", "hooks", map[string]interface{}{"auth_type": "api_key", "scopes": nil, "params": map[string]interface{}{"webhook_secret": "old-hook-secret"}})
	srv := httptest.NewServer(b.handler())
	t.Cleanup(srv.Close)
	t.Setenv("BROKER_BASE_URL", srv.URL)
	t.Setenv("API_KEY", "test-key")
	t.Setenv("GH_ID", "gh-id")
	t.Setenv("GH_SECRET", "gh-secret")
	t.Setenv("WH_SECRET", "new-hook-secret")
	dir := writeFiles(t, map[string]string{"m.yaml": planManifest})
	return b, filepath.Join(dir, "m.yaml")
}

func TestPlan_ReportsCreateUpdateOrphanWithoutMutating(t *testing.T) {
	b, manifest := setupBroker(t)
	var out, errOut bytes.Buffer
	code := run([]string{"plan", "-file", manifest}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d; err=%s", code, errOut.String())
	}
	for _, want := range []string{
		"Read 4 providers from " + manifest,
		"= OK     : github (no changes)",
		"~ UPDATE : salesforce\n    scopes: [a] → [a, b]",
		"+ CREATE : brand-new",
		"~ UPDATE : hooks\n    params: *** → ***",
		"! ORPHAN : old-provider (would be deleted if -prune was passed)",
		"Plan complete. Run 'nexus apply' to perform these actions.",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plan output missing %q:\n%s", want, out.String())
		}
	}
	for _, secret := range []string{"new-hook-secret", "old-hook-secret", "gh-secret", "sf-secret"} {
		if strings.Contains(out.String(), secret) {
			t.Errorf("plan output leaks %q:\n%s", secret, out.String())
		}
	}
	if len(b.mutations) != 0 {
		t.Fatalf("plan must not mutate, got %v", b.mutations)
	}
	if !b.agents["nexus-cli/dev"] || len(b.agents) != 1 {
		t.Fatalf("User-Agent seen: %v, want only nexus-cli/dev", b.agents)
	}
	if !b.apiKeys["test-key"] || len(b.apiKeys) != 1 {
		t.Fatalf("X-API-Key seen: %v", b.apiKeys)
	}
}

func TestPlan_PruneListsDeletionAndNoChangesExitsZero(t *testing.T) {
	b, manifest := setupBroker(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"plan", "-file", manifest, "-prune"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit = %d; err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "- DELETE : old-provider") {
		t.Fatalf("output:\n%s", out.String())
	}
	// Bring the Broker in line and plan again: nothing to do.
	b.add("id-new", "brand-new", map[string]interface{}{"auth_type": "api_key", "scopes": nil})
	b.providers["id-sf"]["scopes"] = []interface{}{"a", "b"}
	b.providers["id-hooks"]["params"] = map[string]interface{}{"webhook_secret": "new-hook-secret"}
	delete(b.providers, "id-old")
	out.Reset()
	if code := run([]string{"plan", "-file", manifest, "-prune"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit = %d; err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "No changes required. Infrastructure matches configuration.") {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestApply_AutoApprovePruneMutatesInOrder(t *testing.T) {
	b, manifest := setupBroker(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"apply", "-file", manifest, "-prune", "-auto-approve"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit = %d; out=%s err=%s", code, out.String(), errOut.String())
	}
	if len(b.mutations) != 4 {
		t.Fatalf("mutations = %v, want create, two updates, delete", b.mutations)
	}
	if !strings.HasPrefix(b.mutations[0], `POST /providers {"profile":{"name":"brand-new","auth_type":"api_key"`) {
		t.Errorf("create: %s", b.mutations[0])
	}
	if b.mutations[1] != `PATCH /providers/id-hooks {"params":{"webhook_secret":"new-hook-secret"}}` {
		t.Errorf("updates go in name order, hooks first: %s", b.mutations[1])
	}
	if b.mutations[2] != `PATCH /providers/id-sf {"scopes":["a","b"]}` {
		t.Errorf("update must send only the drifted field: %s", b.mutations[2])
	}
	if b.mutations[3] != "DELETE /providers/id-old" {
		t.Errorf("delete: %s", b.mutations[3])
	}
	for _, want := range []string{"-auto-approve set; applying without confirmation.", "Creating brand-new... OK", "Updating hooks... OK", "Updating salesforce... OK", "Deleting old-provider... OK", "Apply complete."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestApply_PromptDeclinedMakesNoChanges(t *testing.T) {
	b, manifest := setupBroker(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"apply", "-file", manifest}, strings.NewReader("no\n"), &out, &errOut); code != 0 {
		t.Fatalf("exit = %d; err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Only 'yes' will be accepted") || !strings.Contains(out.String(), "Apply cancelled.") {
		t.Fatalf("output:\n%s", out.String())
	}
	if len(b.mutations) != 0 {
		t.Fatalf("declined apply must not mutate, got %v", b.mutations)
	}
}

func TestApply_FailureIsSummarisedAndExitsOne(t *testing.T) {
	b, manifest := setupBroker(t)
	b.failPost = 400
	var out, errOut bytes.Buffer
	code := run([]string{"apply", "-file", manifest, "-auto-approve"}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; out=%s", code, out.String())
	}
	for _, want := range []string{
		"Creating brand-new... FAILED (Status 400):",
		"Updating hooks... OK",
		"Updating salesforce... OK",
		"Apply completed with 1 error(s):\n  ✗ brand-new: FAILED (Status 400): {\"error\":\"invalid_profile\",\"message\":\"client_secret: missing required field\"}",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestPlan_BrokerUnreachableNamesTheURL(t *testing.T) {
	_, manifest := setupBroker(t)
	t.Setenv("BROKER_BASE_URL", "http://127.0.0.1:1")
	var out, errOut bytes.Buffer
	if code := run([]string{"plan", "-file", manifest}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "Failed to fetch live providers") || !strings.Contains(errOut.String(), "127.0.0.1:1/providers") {
		t.Fatalf("stderr:\n%s", errOut.String())
	}
}

func TestPlan_UnresolvedPlaceholderStopsBeforeBroker(t *testing.T) {
	b, manifest := setupBroker(t)
	t.Setenv("GH_SECRET", "")
	// t.Setenv cannot unset; use a name nothing sets.
	dir := writeFiles(t, map[string]string{"m.yaml": strings.Replace(planManifest, "${GH_SECRET}", "${NOBODY_SETS_THIS}", 1)})
	manifest = filepath.Join(dir, "m.yaml")
	var out, errOut bytes.Buffer
	if code := run([]string{"plan", "-file", manifest}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1; out=%s", code, out.String())
	}
	if !strings.Contains(errOut.String(), "${NOBODY_SETS_THIS}: not found (looked for environment variable NOBODY_SETS_THIS)") {
		t.Fatalf("stderr:\n%s", errOut.String())
	}
	if len(b.agents) != 0 {
		t.Fatal("the Broker must not be contacted when a placeholder is unresolved")
	}
}

func TestPlan_InvalidManifestStopsBeforeBroker(t *testing.T) {
	b, _ := setupBroker(t)
	dir := writeFiles(t, map[string]string{"m.yaml": "providers:\n  - name: Bad Name\n    auth_type: api_key\n"})
	var out, errOut bytes.Buffer
	if code := run([]string{"plan", "-file", filepath.Join(dir, "m.yaml")}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), `invalid provider name "Bad Name"`) || !strings.Contains(errOut.String(), "the Broker would reject it") {
		t.Fatalf("stderr:\n%s", errOut.String())
	}
	if len(b.agents) != 0 {
		t.Fatal("the Broker must not be contacted for an invalid manifest")
	}
}

func TestRun_BadFlagIsUsageError(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"plan", "-bogus"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("exit = %d, want 2; err=%s", code, errOut.String())
	}
	if code := run([]string{"plan", "-h"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("-h exit = %d, want 0", code)
	}
}

func TestDrift_ExplicitEmptyClearsOmittedLeavesAlone(t *testing.T) {
	live := map[string]interface{}{"id": "x", "name": "p", "description": "old", "category": "crm", "enable_discovery": false, "scopes": []interface{}{}}
	empty := ""
	drifted, updates, err := computeDrift(Provider{Name: "p", Description: &empty}, live)
	if err != nil || !drifted || updates["description"] != "" || len(updates) != 1 {
		t.Fatalf("explicit empty description must be sent to clear it: drifted=%v updates=%v err=%v", drifted, updates, err)
	}
	drifted, updates, err = computeDrift(Provider{Name: "p"}, live)
	if err != nil || drifted {
		t.Fatalf("omitted description/category must be left as the Broker has them: drifted=%v updates=%v err=%v", drifted, updates, err)
	}
}
