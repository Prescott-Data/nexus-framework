package cli

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// selfSignedPEM is a throwaway certificate for the SAML format checks.
func selfSignedPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "idp"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func load(t *testing.T, doc string) loaded {
	t.Helper()
	dir := writeFiles(t, map[string]string{"m.yaml": doc})
	l, err := readManifest(filepath.Join(dir, "m.yaml"), keepPlaceholders)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func messages(issues []issue) []string {
	out := make([]string, len(issues))
	for i, is := range issues {
		out[i] = is.Provider + ": " + is.Message
	}
	return out
}

func TestValidate_MirrorsBrokerRules(t *testing.T) {
	l := load(t, `
providers:
  - name: Bad Name
    auth_type: oauth2
    client_id: x
    client_secret: y
    auth_url: a
    token_url: b
  - name: no-creds
    auth_type: oauth2
    issuer: https://issuer
  - name: discovery-no-issuer
    auth_type: oauth2
    client_id: x
    client_secret: y
    enable_discovery: true
  - name: issuer-without-discovery
    auth_type: oauth2
    client_id: x
    client_secret: y
    issuer: https://issuer
  - name: placeholders-count-as-present
    client_id: ${ID}
    client_secret: "${SECRET}"
    auth_url: a
    token_url: b
  - name: static
    auth_type: api_key
  - name: saml-missing
    auth_type: saml
    saml_idp_entity_id: e
  - name: unknown
    auth_type: ldap
  - name: static
    auth_type: basic_auth
  - auth_type: oauth2
`)
	got := messages(validateManifest(l))
	want := []string{
		`Bad Name: name: invalid provider name "Bad Name", must contain only lowercase letters, numbers, and hyphens`,
		"no-creds: client_id: missing required field",
		"no-creds: client_secret: missing required field",
		"no-creds: enable_discovery: must be set to true when using issuer without auth_url/token_url",
		"no-creds: auth_url: missing required field",
		"no-creds: token_url: missing required field",
		"discovery-no-issuer: issuer: required when enable_discovery is true",
		"issuer-without-discovery: enable_discovery: must be set to true when using issuer without auth_url/token_url",
		"issuer-without-discovery: auth_url: missing required field",
		"issuer-without-discovery: token_url: missing required field",
		"saml-missing: saml_idp_sso_url: missing required field for saml",
		"saml-missing: saml_idp_x509_cert: missing required field for saml",
		"saml-missing: saml_sp_entity_id: missing required field for saml",
		`unknown: auth_type: unsupported value "ldap"`,
		"static: name: also defined in " + l.Sources[0],
		": providers[9]: name: missing required field",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("issues:\n%s\n\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestValidate_CleanManifestHasNoIssues(t *testing.T) {
	l := load(t, `
providers:
  - name: google-workspace
    auth_type: oauth2
    client_id: ${G_ID}
    client_secret: ${G_SECRET}
    issuer: https://accounts.google.com
    enable_discovery: true
    scopes: [openid]
  - name: s3
    auth_type: aws_sigv4
  - name: okta-saml
    auth_type: saml
    saml_idp_entity_id: e
    saml_idp_sso_url: https://sso.example.com/saml
    saml_idp_x509_cert: |
`+indent(selfSignedPEM(t), "      ")+`
    saml_sp_entity_id: sp
  - name: saml-with-placeholders
    auth_type: saml
    saml_idp_entity_id: ${E}
    saml_idp_sso_url: ${SSO}
    saml_idp_x509_cert: ${CERT}
    saml_sp_entity_id: ${SP}
`)
	if issues := validateManifest(l); len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
}

func TestValidate_SAMLFormatRulesMirrorBroker(t *testing.T) {
	l := load(t, `
providers:
  - name: bad-saml
    auth_type: saml
    saml_idp_entity_id: e
    saml_idp_sso_url: sso.example.com/no-scheme
    saml_idp_x509_cert: not-a-cert
    saml_sp_entity_id: sp
  - name: ftp-saml
    auth_type: saml
    saml_idp_entity_id: e
    saml_idp_sso_url: ftp://sso.example.com
    saml_idp_x509_cert: |
      -----BEGIN PRIVATE KEY-----
      AAAA
      -----END PRIVATE KEY-----
    saml_sp_entity_id: sp
`)
	got := messages(validateManifest(l))
	want := []string{
		"bad-saml: saml_idp_sso_url must use http or https",
		"bad-saml: saml_idp_x509_cert must be PEM or base64 DER: illegal base64 data at input byte 3",
		"ftp-saml: saml_idp_sso_url must use http or https",
		"ftp-saml: saml_idp_x509_cert must contain a CERTIFICATE PEM block",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("issues:\n%s\n\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRunValidate_IgnoresNexusSecretsEnv(t *testing.T) {
	// $NEXUS_SECRETS must not make validate decrypt or contact anything.
	dir := writeFiles(t, map[string]string{"github.yaml": githubFile})
	t.Setenv("NEXUS_SECRETS", "sops:/nonexistent.enc.yaml")
	var out, errOut bytes.Buffer
	if code := run([]string{"validate", "-file", dir}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit = %d; out=%s err=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "not resolved; pass -secrets") {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestRunValidate_DirectoryReportsFileAndExitCode(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"github.yaml": githubFile,
		"broken.yaml": "name: broken\nauth_type: oauth2\nclient_id: x\n",
	})
	var out, errOut bytes.Buffer
	code := run([]string{"validate", "-file", dir}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; out=%s err=%s", code, out.String(), errOut.String())
	}
	s := out.String()
	for _, want := range []string{
		"Read 2 providers from " + dir + " (2 files)",
		filepath.Join(dir, "broken.yaml") + ": broken: client_secret: missing required field",
		"2 placeholders used (not resolved; pass -secrets to check them)",
		"✗ 3 problem(s)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
}

func TestRunValidate_WithSecretsFailsOnUnresolved(t *testing.T) {
	dir := writeFiles(t, map[string]string{"github.yaml": githubFile})
	t.Setenv("NEXUS_PROVIDER_GITHUB_CLIENT_ID", "id")
	var out, errOut bytes.Buffer
	code := run([]string{"validate", "-file", dir, "-secrets", "env"}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; out=%s err=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "${NEXUS_PROVIDER_GITHUB_CLIENT_SECRET}: not found (looked for environment variable NEXUS_PROVIDER_GITHUB_CLIENT_SECRET)") {
		t.Fatalf("output:\n%s", out.String())
	}
	// A placeholder that resolves to an empty string fails the Broker's
	// rule on the expanded manifest, exactly as plan would.
	t.Setenv("NEXUS_PROVIDER_GITHUB_CLIENT_SECRET", "")
	out.Reset()
	if code := run([]string{"validate", "-file", dir, "-secrets", "env"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("empty secret: exit = %d, want 1; out=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "github: client_secret: missing required field") {
		t.Fatalf("output:\n%s", out.String())
	}
	t.Setenv("NEXUS_PROVIDER_GITHUB_CLIENT_SECRET", "sec")
	out.Reset()
	if code := run([]string{"validate", "-file", dir, "-secrets", "env"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, want 0; out=%s err=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "2 placeholders, 2 resolved from env") || !strings.Contains(out.String(), "✓ Manifest is valid") {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestRun_UsageAndVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "Usage: nexus") {
		t.Fatalf("no args: code=%d err=%s", code, errOut.String())
	}
	errOut.Reset()
	if code := run([]string{"bogus"}, strings.NewReader(""), &out, &errOut); code != 2 || !strings.Contains(errOut.String(), `unknown command "bogus"`) {
		t.Fatalf("bogus: code=%d err=%s", code, errOut.String())
	}
	if code := run([]string{"version"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "nexus dev") {
		t.Fatalf("version: code=%d out=%s", code, out.String())
	}
}
