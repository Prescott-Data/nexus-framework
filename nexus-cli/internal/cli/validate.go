package cli

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

// validNamePattern mirrors the Broker: lowercase letters, digits, hyphens.
var validNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// staticAuthTypes need only a name on the Broker; the credential schema and
// strategy live in params and are validated when a connection is captured.
var staticAuthTypes = map[string]bool{
	"api_key": true, "basic_auth": true, "header": true,
	"query_param": true, "hmac_payload": true, "aws_sigv4": true,
}

// issue is one validation finding, attributed to the file it came from.
type issue struct {
	Source   string
	Provider string
	Message  string
}

func (i issue) String() string {
	if i.Provider == "" {
		return fmt.Sprintf("%s: %s", i.Source, i.Message)
	}
	return fmt.Sprintf("%s: %s: %s", i.Source, i.Provider, i.Message)
}

// validateManifest applies the Broker's registration rules offline, so a
// manifest that would be rejected fails here, in a pull request, instead of
// halfway through an apply. Placeholders are treated as present values: the
// check is about shape, and whether a secret resolves is a separate concern.
func validateManifest(l loaded) []issue {
	var issues []issue
	// Every message passes through redact: a finding may quote a value
	// (an unsupported auth_type, an invalid name) that came from a secret.
	add := func(i int, msg string) {
		issues = append(issues, issue{Source: l.Sources[i], Provider: l.redact(l.Manifest.Providers[i].Name), Message: l.redact(msg)})
	}
	definedIn := map[string]int{}
	for i, p := range l.Manifest.Providers {
		switch {
		case p.Name == "":
			issues = append(issues, issue{Source: l.Sources[i], Message: fmt.Sprintf("providers[%d]: name: missing required field", i)})
			continue
		case !validNamePattern.MatchString(p.Name):
			add(i, fmt.Sprintf("name: invalid provider name %q, must contain only lowercase letters, numbers, and hyphens", p.Name))
		}
		if prev, dup := definedIn[p.Name]; dup {
			add(i, fmt.Sprintf("name: also defined in %s", l.Sources[prev]))
		} else {
			definedIn[p.Name] = i
		}

		switch at := p.AuthType; {
		case at == "" || at == "oauth2":
			if p.ClientID == "" {
				add(i, "client_id: missing required field")
			}
			if strings.TrimSpace(p.ClientSecret) == "" {
				add(i, "client_secret: missing required field")
			}
			if p.EnableDiscovery {
				if strings.TrimSpace(p.Issuer) == "" {
					add(i, "issuer: required when enable_discovery is true")
				}
			} else {
				if p.Issuer != "" && p.AuthURL == "" && p.TokenURL == "" {
					add(i, "enable_discovery: must be set to true when using issuer without auth_url/token_url")
				}
				if p.AuthURL == "" {
					add(i, "auth_url: missing required field")
				}
				if p.TokenURL == "" {
					add(i, "token_url: missing required field")
				}
			}
		case at == "saml":
			for _, f := range []struct{ name, value string }{
				{"saml_idp_entity_id", p.SAMLIdpEntityID},
				{"saml_idp_sso_url", p.SAMLIdpSSOURL},
				{"saml_idp_x509_cert", p.SAMLIdpX509Cert},
				{"saml_sp_entity_id", p.SAMLSPEntityID},
			} {
				if strings.TrimSpace(f.value) == "" {
					add(i, f.name+": missing required field for saml")
				}
			}
			// Format rules the Broker applies when it builds the service
			// provider (samlutil). A value that is still a placeholder is
			// unknown here and is left to the Broker.
			if v := p.SAMLIdpSSOURL; strings.TrimSpace(v) != "" && !isPlaceholder(v) {
				if msg := checkHTTPURL("saml_idp_sso_url", v); msg != "" {
					add(i, msg)
				}
			}
			if v := p.SAMLIdpX509Cert; strings.TrimSpace(v) != "" && !isPlaceholder(v) {
				if msg := checkCertificate(v); msg != "" {
					add(i, msg)
				}
			}
		case staticAuthTypes[at]:
			// Only the name is required.
		default:
			add(i, fmt.Sprintf("auth_type: unsupported value %q", at))
		}
	}
	return issues
}

// isPlaceholder reports whether a value still contains an unexpanded
// ${NAME}, as validate sees them.
func isPlaceholder(v string) bool { return strings.Contains(v, "${") }

// exactPlaceholder matches a value that is one placeholder and nothing
// else: ${NAME} or $NAME, as os.Expand reads them.
var exactPlaceholder = regexp.MustCompile(`^\$(\{[A-Za-z_][A-Za-z0-9_]*\}|[A-Za-z_][A-Za-z0-9_]*)$`)

// literalSecretIssues is the -require-encrypted-secrets rule on the
// manifest as written (placeholders kept): a client_secret must be exactly
// one placeholder. A literal value is plaintext at rest in the repository
// however the other secrets are stored, and so is a literal with a
// placeholder mixed in ("hunter2-${X}"), which would survive a
// contains-"${" check and still be the whole secret once X resolves to
// nothing. The value itself is never part of the message. params is not
// policed: whether a key under it is a credential is provider-specific,
// and plan masks it anyway.
func literalSecretIssues(l loaded) []issue {
	var issues []issue
	for i, p := range l.Manifest.Providers {
		if v := strings.TrimSpace(p.ClientSecret); v != "" && !exactPlaceholder.MatchString(v) {
			issues = append(issues, issue{Source: l.Sources[i], Provider: p.Name, Message: "client_secret: not exactly one ${PLACEHOLDER}; -require-encrypted-secrets refuses a literal or partly literal value in the manifest"})
		}
	}
	return issues
}

// checkHTTPURL mirrors samlutil.parseHTTPSURL.
func checkHTTPURL(name, raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		// *url.Error prints the whole input; the value may be a resolved
		// secret, so report only the underlying reason.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Sprintf("%s is invalid: %v", name, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return name + " must use http or https"
	}
	if u.Host == "" {
		return name + " must include a host"
	}
	return ""
}

// checkCertificate mirrors samlutil.NormalizeCertificate: a CERTIFICATE PEM
// block or base64 DER, and it must parse as X.509.
func checkCertificate(raw string) string {
	cert := strings.TrimSpace(raw)
	var der []byte
	if block, _ := pem.Decode([]byte(cert)); block != nil {
		if block.Type != "CERTIFICATE" {
			return "saml_idp_x509_cert must contain a CERTIFICATE PEM block"
		}
		der = block.Bytes
	} else {
		compact := strings.NewReplacer("\r", "", "\n", "", "\t", "", " ", "").Replace(cert)
		decoded, err := base64.StdEncoding.DecodeString(compact)
		if err != nil {
			return fmt.Sprintf("saml_idp_x509_cert must be PEM or base64 DER: %v", err)
		}
		der = decoded
	}
	if _, err := x509.ParseCertificate(der); err != nil {
		return fmt.Sprintf("saml_idp_x509_cert is not a valid X.509 certificate: %v", err)
	}
	return ""
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	var mf manifestFlags
	addManifestFlags(fs, &mf)
	// For validate, -secrets is opt-in: without it the manifest is checked
	// as written and placeholders are only listed, so the check runs in a
	// pull request that has no access to secrets. $NEXUS_SECRETS does not
	// apply here, so a validate in CI never decrypts or contacts anything
	// unless the flag is given.
	mf.secrets = ""
	fs.Lookup("secrets").DefValue = ""
	fs.Lookup("secrets").Usage = "Also resolve every ${PLACEHOLDER} from this source and fail on any that is missing: env, sops:<path> or gcp-secret-manager:<project> ($NEXUS_SECRETS is not used by validate)"
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}

	l, err := readManifest(mf.file, keepPlaceholders)
	if err != nil {
		fmt.Fprintf(stderr, "✗ %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, readSummary(l, mf.file))

	// -require-encrypted-secrets on the manifest alone: no literal
	// client_secret. This part needs no source, so it runs in a pull
	// request; the source itself is checked below when -secrets is given.
	var literal []issue
	if mf.requireEncrypted {
		literal = literalSecretIssues(l)
		if mf.secrets == "" {
			fmt.Fprintln(stdout, "-require-encrypted-secrets: manifest checked for literal secrets (pass -secrets to also check the source)")
		}
	}

	// With -secrets the rules run on the expanded manifest, exactly what
	// plan and apply would send, so a secret that resolves to an empty
	// string or a SAML certificate that resolves to junk fails here too.
	// Without it they run on the manifest as written, placeholders present.
	checked := l
	unresolved := 0
	if mf.secrets != "" {
		src, err := newSecretSource(mf.secrets, mf.secretPrefix)
		if err != nil {
			fmt.Fprintf(stderr, "✗ %v\n", err)
			return 1
		}
		if mf.requireEncrypted {
			if err := requireEncryptedSource(src); err != nil {
				fmt.Fprintf(stderr, "✗ %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "-require-encrypted-secrets: %s is encrypted at rest; the environment is not consulted\n", src)
		}
		lookup, err := lookupWith(context.Background(), src, l.Placeholders, !mf.requireEncrypted)
		if err != nil {
			fmt.Fprintf(stderr, "✗ %v\n", err)
			return 1
		}
		if checked, err = readManifest(mf.file, lookup); err != nil {
			fmt.Fprintf(stderr, "✗ %v\n", err)
			return 1
		}
		if unresolved = len(checked.Missing); unresolved > 0 {
			fmt.Fprintf(stdout, "✗ %s\n", unresolvedMessage(src, checked.Missing, !mf.requireEncrypted))
		}
		fmt.Fprintf(stdout, "%d placeholders, %d resolved from %s\n", len(l.Placeholders), len(l.Placeholders)-unresolved, src)
	} else if len(l.Placeholders) > 0 {
		fmt.Fprintf(stdout, "%d placeholders used (not resolved; pass -secrets to check them)\n", len(l.Placeholders))
	}

	issues := append(literal, validateManifest(checked)...)
	for _, i := range issues {
		fmt.Fprintf(stdout, "✗ %s\n", i)
	}

	if len(issues) > 0 || unresolved > 0 {
		fmt.Fprintf(stdout, "\n✗ %d problem(s)\n", len(issues)+unresolved)
		return 1
	}
	fmt.Fprintln(stdout, "✓ Manifest is valid")
	return 0
}
