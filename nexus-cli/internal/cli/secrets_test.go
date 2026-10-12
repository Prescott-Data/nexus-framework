package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSecretIDFor(t *testing.T) {
	cases := []struct{ in, prefix, want string }{
		{"NEXUS_PROVIDER_TWITTER_CLIENT_ID", "oauth-", "oauth-twitter-client-id"},
		{"NEXUS_PROVIDER_TWITTER_ADS_CLIENT_SECRET", "oauth-", "oauth-twitter-ads-client-secret"},
		{"GITHUB_CLIENT_SECRET", "", "github-client-secret"},
		{"NEXUS_PROVIDER_X", "", "x"},
	}
	for _, c := range cases {
		if got := secretIDFor(c.in, c.prefix); got != c.want {
			t.Errorf("secretIDFor(%q, %q) = %q, want %q", c.in, c.prefix, got, c.want)
		}
	}
}

func TestNewSecretSource(t *testing.T) {
	for _, spec := range []string{"", "env"} {
		s, err := newSecretSource(spec, "")
		if err != nil || s.String() != "env" {
			t.Fatalf("%q: %v %v", spec, s, err)
		}
	}
	s, err := newSecretSource("sops:secrets.enc.yaml", "")
	if err != nil || s.String() != "sops:secrets.enc.yaml" {
		t.Fatalf("sops: %v %v", s, err)
	}
	s, err = newSecretSource("gcp-secret-manager:proj", "oauth-")
	if err != nil || s.String() != "gcp-secret-manager:proj" {
		t.Fatalf("gcp: %v %v", s, err)
	}
	for _, bad := range []string{"sops:", "gcp-secret-manager:", "vault:x", "aws"} {
		if _, err := newSecretSource(bad, ""); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestEnvSourceAndFallback(t *testing.T) {
	t.Setenv("ONLY_IN_ENV", "from-env")
	src := &sopsSource{path: "s.yaml", decryptFile: func(string, string) ([]byte, error) {
		return []byte("IN_SOPS: from-sops\n"), nil
	}}
	lookup, err := lookupWith(context.Background(), src, []string{"IN_SOPS", "ONLY_IN_ENV", "NOWHERE"})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := lookup("IN_SOPS"); !ok || v != "from-sops" {
		t.Fatalf("IN_SOPS = %q,%v", v, ok)
	}
	if v, ok := lookup("ONLY_IN_ENV"); !ok || v != "from-env" {
		t.Fatalf("env fallback: %q,%v", v, ok)
	}
	if _, ok := lookup("NOWHERE"); ok {
		t.Fatal("NOWHERE must be unresolved")
	}
}

func TestSopsSource_FormatsAndScalars(t *testing.T) {
	cases := []struct {
		path, plain string
		want        map[string]string
	}{
		{"s.yaml", "A: one\nB: 2\nC: true\nD:\nsops: {version: 3}\n", map[string]string{"A": "one", "B": "2", "C": "true", "D": ""}},
		{"s.json", `{"A":"one","B":2,"sops":{"version":"3"}}`, map[string]string{"A": "one", "B": "2"}},
		{"s.env", "# comment\nA=one\nB=\"two\"\nsops_version=3.8\n", map[string]string{"A": "one", "B": "two"}},
	}
	for _, c := range cases {
		src := &sopsSource{path: c.path, decryptFile: func(path, format string) ([]byte, error) {
			if path != c.path {
				t.Fatalf("decrypt path = %s", path)
			}
			return []byte(c.plain), nil
		}}
		names := make([]string, 0, len(c.want)+1)
		for k := range c.want {
			names = append(names, k)
		}
		names = append(names, "ABSENT")
		got, err := src.Resolve(context.Background(), names)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v, want %v", c.path, got, c.want)
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("%s: %s = %q, want %q", c.path, k, got[k], v)
			}
		}
	}
}

func TestSopsSource_RejectsNestedAndUnknownExt(t *testing.T) {
	src := &sopsSource{path: "s.yaml", decryptFile: func(string, string) ([]byte, error) {
		return []byte("A:\n  nested: 1\n"), nil
	}}
	if _, err := src.Resolve(context.Background(), []string{"A"}); err == nil || !strings.Contains(err.Error(), "must be a scalar") {
		t.Fatalf("err = %v, want scalar error", err)
	}
	src = &sopsSource{path: "s.toml"}
	if _, err := src.Resolve(context.Background(), []string{"A"}); err == nil || !strings.Contains(err.Error(), "unsupported SOPS file type") {
		t.Fatalf("err = %v, want unsupported type", err)
	}
	src = &sopsSource{path: "s.yaml", decryptFile: func(string, string) ([]byte, error) { return nil, errors.New("no key") }}
	if _, err := src.Resolve(context.Background(), []string{"A"}); err == nil || !strings.Contains(err.Error(), "decrypt s.yaml: no key") {
		t.Fatalf("err = %v, want decrypt error", err)
	}
}

func TestSopsSource_RealDecryptRejectsPlaintextFile(t *testing.T) {
	// A file without SOPS metadata must not be accepted as if it were
	// encrypted; this exercises the real decrypt path with no keys needed.
	path := filepath.Join(t.TempDir(), "plain.yaml")
	if err := os.WriteFile(path, []byte("A: one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := &sopsSource{path: path}
	if _, err := src.Resolve(context.Background(), []string{"A"}); err == nil {
		t.Fatal("want an error for a file that is not SOPS-encrypted")
	}
}

// fakeAccessor is an in-memory Secret Manager: secret id → payload.
type fakeAccessor struct {
	mu      sync.Mutex
	secrets map[string]string
	calls   []string
	fail    error
	closed  bool
}

func (f *fakeAccessor) AccessSecretVersion(_ context.Context, req *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req.Name)
	if f.fail != nil {
		return nil, f.fail
	}
	// projects/P/secrets/ID/versions/latest
	parts := strings.Split(req.Name, "/")
	id := parts[3]
	v, ok := f.secrets[id]
	if !ok {
		return nil, status.Error(codes.NotFound, "not found")
	}
	return &secretmanagerpb.AccessSecretVersionResponse{Payload: &secretmanagerpb.SecretPayload{Data: []byte(v)}}, nil
}

func (f *fakeAccessor) Close() error { f.closed = true; return nil }

func TestGCPSource_DerivesIDsReadsLatestAndSkipsNotFound(t *testing.T) {
	fake := &fakeAccessor{secrets: map[string]string{
		"oauth-twitter-client-id":     "tid",
		"oauth-twitter-client-secret": "tsec",
	}}
	src := &gcpSource{project: "proj", prefix: "oauth-", open: func(context.Context) (secretAccessor, error) { return fake, nil }}
	got, err := src.Resolve(context.Background(), []string{
		"NEXUS_PROVIDER_TWITTER_CLIENT_ID", "NEXUS_PROVIDER_TWITTER_CLIENT_SECRET", "NEXUS_PROVIDER_SLACK_CLIENT_ID",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["NEXUS_PROVIDER_TWITTER_CLIENT_ID"] != "tid" || got["NEXUS_PROVIDER_TWITTER_CLIENT_SECRET"] != "tsec" {
		t.Fatalf("got %v", got)
	}
	if _, ok := got["NEXUS_PROVIDER_SLACK_CLIENT_ID"]; ok {
		t.Fatal("a NotFound secret must be absent, not an error")
	}
	if len(fake.calls) != 3 || !fake.closed {
		t.Fatalf("calls=%d closed=%v; want 3 reads and Close", len(fake.calls), fake.closed)
	}
	for _, c := range fake.calls {
		if !strings.HasPrefix(c, "projects/proj/secrets/oauth-") || !strings.HasSuffix(c, "/versions/latest") {
			t.Errorf("unexpected resource name %s", c)
		}
	}
}

func TestGCPSource_PermissionErrorIsFatal(t *testing.T) {
	fake := &fakeAccessor{fail: status.Error(codes.PermissionDenied, "denied")}
	src := &gcpSource{project: "proj", open: func(context.Context) (secretAccessor, error) { return fake, nil }}
	_, err := src.Resolve(context.Background(), []string{"A"})
	if err == nil || !strings.Contains(err.Error(), "read secret a (for ${A})") {
		t.Fatalf("err = %v, want a fatal read error naming the secret", err)
	}
}

func TestGCPSource_NoPlaceholdersOpensNoClient(t *testing.T) {
	src := &gcpSource{project: "proj", open: func(context.Context) (secretAccessor, error) {
		t.Fatal("client must not be opened when nothing is needed")
		return nil, nil
	}}
	if got, err := src.Resolve(context.Background(), nil); err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}
