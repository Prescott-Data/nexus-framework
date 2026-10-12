package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// touch gives a fake-backed sops source a real file to read; the fake
// decrypt ignores the bytes.
func touch(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(writeFiles(t, map[string]string{name: ""}), name)
}

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
	src := &sopsSource{path: touch(t, "s.yaml"), decryptData: func([]byte, string) ([]byte, error) {
		return []byte("IN_SOPS: from-sops\n"), nil
	}}
	lookup, err := lookupWith(context.Background(), src, []string{"IN_SOPS", "ONLY_IN_ENV", "NOWHERE"}, true)
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
		{"s.yaml", "A: one\nB: 2\nC: true\nD:\nBIG: 9007199254740993\nLEAD: 0012\nsops: {version: 3}\n", map[string]string{"A": "one", "B": "2", "C": "true", "D": "", "BIG": "9007199254740993", "LEAD": "0012"}},
		{"s.json", `{"A":"one","B":2,"BIG":9007199254740993,"F":1.50,"sops":{"version":"3"}}`, map[string]string{"A": "one", "B": "2", "BIG": "9007199254740993", "F": "1.50"}},
		{"s.env", "# comment\nA=one\nB=\"two\"\nsops_version=3.8\n", map[string]string{"A": "one", "B": "two"}},
	}
	for _, c := range cases {
		wantFormat, _ := sopsFormat(c.path)
		src := &sopsSource{path: touch(t, c.path), decryptData: func(_ []byte, format string) ([]byte, error) {
			if format != wantFormat {
				t.Fatalf("decrypt format = %s, want %s", format, wantFormat)
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
	src := &sopsSource{path: touch(t, "s.yaml"), decryptData: func([]byte, string) ([]byte, error) {
		return []byte("A:\n  nested: 1\n"), nil
	}}
	if _, err := src.Resolve(context.Background(), []string{"A"}); err == nil || !strings.Contains(err.Error(), "must be a scalar") {
		t.Fatalf("err = %v, want scalar error", err)
	}
	src = &sopsSource{path: "s.toml"}
	if _, err := src.Resolve(context.Background(), []string{"A"}); err == nil || !strings.Contains(err.Error(), "unsupported SOPS file type") {
		t.Fatalf("err = %v, want unsupported type", err)
	}
	src = &sopsSource{path: touch(t, "s.yaml"), decryptData: func([]byte, string) ([]byte, error) { return nil, errors.New("no key") }}
	if _, err := src.Resolve(context.Background(), []string{"A"}); err == nil || !strings.Contains(err.Error(), "decrypt s.yaml: no key") {
		t.Fatalf("err = %v, want decrypt error", err)
	}
}

func TestSopsSource_DotenvErrorDoesNotEchoTheLine(t *testing.T) {
	src := &sopsSource{path: touch(t, "s.env"), decryptData: func([]byte, string) ([]byte, error) {
		return []byte("A=fine\n\nhunter2-without-equals\n"), nil
	}}
	_, err := src.Resolve(context.Background(), []string{"A"})
	if err == nil || !strings.Contains(err.Error(), "dotenv line 3: expected KEY=value") {
		t.Fatalf("err = %v, want a line-number-only error", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error echoes decrypted content: %v", err)
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

func TestUnresolvedMessage_NamesWhatWasLookedFor(t *testing.T) {
	gcp := &gcpSource{project: "proj", prefix: "oauth-"}
	got := unresolvedMessage(gcp, []string{"NEXUS_PROVIDER_TWITTER_CLIENT_ID"}, true)
	want := "1 placeholder(s) could not be resolved from gcp-secret-manager:proj:\n" +
		"  ${NEXUS_PROVIDER_TWITTER_CLIENT_ID}: not found (looked for secret oauth-twitter-client-id in project proj, then environment variable NEXUS_PROVIDER_TWITTER_CLIENT_ID)"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	sops := &sopsSource{path: "s.enc.yaml"}
	if got := unresolvedMessage(sops, []string{"A"}, true); !strings.Contains(got, "looked for key A in s.enc.yaml, then environment variable A") {
		t.Fatalf("sops: %s", got)
	}
	if got := unresolvedMessage(envSource{}, []string{"A"}, true); !strings.Contains(got, "looked for environment variable A)") || strings.Contains(got, "then") {
		t.Fatalf("env: %s", got)
	}
}

// writeCiphertextFile writes a file shaped like a SOPS-encrypted one: every
// listed key holds an ENC[...] value except those in plain, plus a sops
// metadata block. Decryption is faked by the tests that use it.
func writeCiphertextFile(t *testing.T, name string, enc []string, plain map[string]string) string {
	t.Helper()
	var b strings.Builder
	for _, k := range enc {
		fmt.Fprintf(&b, "%s: ENC[AES256_GCM,data:abc,iv:def,tag:ghi,type:str]\n", k)
	}
	for k, v := range plain {
		fmt.Fprintf(&b, "%s: %s\n", k, v)
	}
	b.WriteString("sops:\n    version: 3.13.3\n    lastmodified: \"2026-10-12T00:00:00Z\"\n")
	dir := writeFiles(t, map[string]string{name: b.String()})
	return filepath.Join(dir, name)
}

func TestSopsSource_RequireCiphertextRefusesPlaintextValues(t *testing.T) {
	// A file whose client secret was left in plaintext by an
	// unencrypted_suffix rule still decrypts; the policy must catch it.
	path := writeCiphertextFile(t, "s.enc.yaml", []string{"ID"}, map[string]string{"SECRET_unencrypted": "hunter2", "OTHER_unencrypted": "x"})
	decrypted := func([]byte, string) ([]byte, error) {
		return []byte("ID: id\nSECRET_unencrypted: hunter2\nOTHER_unencrypted: x\n"), nil
	}
	src := &sopsSource{path: path, decryptData: decrypted}
	if _, err := src.Resolve(context.Background(), []string{"ID", "SECRET_unencrypted"}); err != nil {
		t.Fatalf("without the policy a plaintext value is the user's choice: %v", err)
	}
	if err := requireEncryptedSource(src); err != nil || !src.requireCiphertext {
		t.Fatalf("policy not armed: %v", err)
	}
	_, err := src.Resolve(context.Background(), []string{"ID", "SECRET_unencrypted"})
	if err == nil {
		t.Fatal("plaintext value accepted under -require-encrypted-secrets")
	}
	want := "-require-encrypted-secrets: 1 value(s) are stored in plaintext in " + path + " (left unencrypted by a SOPS unencrypted_suffix, unencrypted_regex or encrypted_regex rule): SECRET_unencrypted"
	if err.Error() != want || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error = %q\nwant   %q", err.Error(), want)
	}
	// Only requested names count, and a name absent from the file is not
	// "plaintext": it is unresolved, reported later with the others.
	if got, err := src.Resolve(context.Background(), []string{"ID", "NOWHERE"}); err != nil || got["ID"] != "id" {
		t.Fatalf("got %v, %v", got, err)
	}
	// A value that merely looks like ciphertext is not: SOPS left it
	// untouched, so it decrypts to itself, and that is the test.
	shaped := writeCiphertextFile(t, "shaped.enc.yaml", []string{"ID"}, map[string]string{"SECRET_unencrypted": "ENC[plain-but-not-encrypted]"})
	sh := &sopsSource{path: shaped, decryptData: func([]byte, string) ([]byte, error) {
		return []byte("ID: id\nSECRET_unencrypted: ENC[plain-but-not-encrypted]\n"), nil
	}, requireCiphertext: true}
	if _, err := sh.Resolve(context.Background(), []string{"ID", "SECRET_unencrypted"}); err == nil || !strings.HasSuffix(err.Error(), ": SECRET_unencrypted") {
		t.Fatalf("ciphertext-shaped literal: %v", err)
	}
	// Same rule for the other formats.
	jsonPath := filepath.Join(writeFiles(t, map[string]string{"s.enc.json": `{"ID":"ENC[AES256_GCM,data:a,type:str]","SECRET":"plain","NUM":9007199254740993,"FLAG":true,"NONE":null,"sops":{"version":"3.13.3"}}`}), "s.enc.json")
	js := &sopsSource{path: jsonPath, decryptData: func([]byte, string) ([]byte, error) {
		return []byte(`{"ID":"id","SECRET":"plain","NUM":9007199254740993,"FLAG":true,"NONE":null}`), nil
	}, requireCiphertext: true}
	// A number, boolean or null stored as such is plaintext too: the
	// decrypted parser would accept each as a value.
	if _, err := js.Resolve(context.Background(), []string{"ID", "SECRET", "NUM", "FLAG", "NONE"}); err == nil || !strings.HasSuffix(err.Error(), ": SECRET, NUM, FLAG, NONE") {
		t.Fatalf("json: %v", err)
	}
	envPath := filepath.Join(writeFiles(t, map[string]string{"s.env": "ID=ENC[AES256_GCM,data:a,type:str]\nSECRET=plain\nsops_version=3.13.3\n"}), "s.env")
	de := &sopsSource{path: envPath, decryptData: func([]byte, string) ([]byte, error) { return []byte("ID=id\nSECRET=plain\n"), nil }, requireCiphertext: true}
	if _, err := de.Resolve(context.Background(), []string{"ID", "SECRET"}); err == nil || !strings.HasSuffix(err.Error(), ": SECRET") {
		t.Fatalf("dotenv: %v", err)
	}
}

func TestEncryptedSources_PolicyAndNoEnvFallback(t *testing.T) {
	for _, c := range []struct {
		src  secretSource
		want bool
	}{
		{envSource{}, false},
		{&sopsSource{path: "s.enc.yaml"}, true},
		{&gcpSource{project: "p"}, true},
	} {
		if got := c.src.Encrypted(); got != c.want {
			t.Errorf("%s: Encrypted() = %v, want %v", c.src, got, c.want)
		}
	}
	err := requireEncryptedSource(envSource{})
	if err == nil || !strings.Contains(err.Error(), `-secrets is "env", which is not encrypted at rest; use sops:<path> or gcp-secret-manager:<project>`) {
		t.Fatalf("env source error = %v", err)
	}
	if err := requireEncryptedSource(&sopsSource{path: "s.enc.yaml"}); err != nil {
		t.Fatalf("sops source rejected: %v", err)
	}

	// Without env fallback a value the source lacks stays unresolved even
	// when the environment has it, and the message does not claim the
	// environment was tried.
	t.Setenv("ONLY_IN_ENV", "from-env")
	src := &sopsSource{path: touch(t, "s.yaml"), decryptData: func([]byte, string) ([]byte, error) { return []byte("IN_SOPS: a\n"), nil }}
	lookup, err := lookupWith(context.Background(), src, []string{"IN_SOPS", "ONLY_IN_ENV"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := lookup("IN_SOPS"); !ok || v != "a" {
		t.Fatalf("IN_SOPS = %q, %v", v, ok)
	}
	if _, ok := lookup("ONLY_IN_ENV"); ok {
		t.Fatal("environment consulted although fallback is off")
	}
	if got := unresolvedMessage(src, []string{"ONLY_IN_ENV"}, false); strings.Contains(got, "then environment variable") || !strings.Contains(got, "looked for key ONLY_IN_ENV in s.yaml)") {
		t.Fatalf("message:\n%s", got)
	}
}
