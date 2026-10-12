package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
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
	add := func(i int, msg string) {
		issues = append(issues, issue{Source: l.Sources[i], Provider: l.Manifest.Providers[i].Name, Message: msg})
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
		case staticAuthTypes[at]:
			// Only the name is required.
		default:
			add(i, fmt.Sprintf("auth_type: unsupported value %q", at))
		}
	}
	return issues
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	var mf manifestFlags
	addManifestFlags(fs, &mf)
	// For validate, -secrets is opt-in: without it the manifest is checked
	// as written and placeholders are only listed, so the check runs in a
	// pull request that has no access to secrets.
	fs.Lookup("secrets").DefValue = ""
	fs.Lookup("secrets").Usage = "Also resolve every ${PLACEHOLDER} from this source and fail on any that is missing: env, sops:<path> or gcp-secret-manager:<project>"
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}

	l, err := readManifest(mf.file, keepPlaceholders)
	if err != nil {
		fmt.Fprintf(stderr, "✗ %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, readSummary(l, mf.file))

	issues := validateManifest(l)
	for _, i := range issues {
		fmt.Fprintf(stdout, "✗ %s\n", i)
	}

	unresolved := 0
	if mf.secrets != "" {
		src, err := newSecretSource(mf.secrets, mf.secretPrefix)
		if err != nil {
			fmt.Fprintf(stderr, "✗ %v\n", err)
			return 1
		}
		lookup, err := lookupWith(context.Background(), src, l.Placeholders)
		if err != nil {
			fmt.Fprintf(stderr, "✗ %v\n", err)
			return 1
		}
		for _, name := range l.Placeholders {
			if _, ok := lookup(name); !ok {
				unresolved++
				fmt.Fprintf(stdout, "✗ ${%s}: not found in %s or the environment\n", name, src)
			}
		}
		fmt.Fprintf(stdout, "%d placeholders, %d resolved from %s\n", len(l.Placeholders), len(l.Placeholders)-unresolved, src)
	} else if len(l.Placeholders) > 0 {
		fmt.Fprintf(stdout, "%d placeholders used (not resolved; pass -secrets to check them)\n", len(l.Placeholders))
	}

	if len(issues) > 0 || unresolved > 0 {
		fmt.Fprintf(stdout, "\n✗ %d problem(s)\n", len(issues)+unresolved)
		return 1
	}
	fmt.Fprintln(stdout, "✓ Manifest is valid")
	return 0
}
