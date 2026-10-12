package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/getsops/sops/v3/decrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"
)

// A secretSource resolves placeholder names to values. Resolve is given the
// full set of names the manifest uses, so a remote source fetches only what
// is needed, once. A name the source does not hold is simply absent from the
// result; only transport or permission failures are errors.
type secretSource interface {
	Resolve(ctx context.Context, names []string) (map[string]string, error)
	String() string
	// Where returns what Resolve looked for when it did not find name, so
	// an unresolved placeholder names the exact secret id, file key or
	// variable that was tried.
	Where(name string) string
	// Encrypted reports whether the values are encrypted at rest where
	// this source reads them: a SOPS file and Secret Manager are; the
	// process environment is not. -require-encrypted-secrets refuses a
	// source for which this is false.
	Encrypted() bool
}

// newSecretSource parses a -secrets value. "" and "env" are the environment.
func newSecretSource(spec, prefix string) (secretSource, error) {
	switch {
	case spec == "" || spec == "env":
		return envSource{}, nil
	case strings.HasPrefix(spec, "sops:"):
		path := strings.TrimPrefix(spec, "sops:")
		if path == "" {
			return nil, errors.New("-secrets sops: needs a file path, e.g. sops:secrets.enc.yaml")
		}
		return &sopsSource{path: path}, nil
	case strings.HasPrefix(spec, "gcp-secret-manager:"):
		project := strings.TrimPrefix(spec, "gcp-secret-manager:")
		if project == "" {
			return nil, errors.New("-secrets gcp-secret-manager: needs a project id, e.g. gcp-secret-manager:my-project")
		}
		return &gcpSource{project: project, prefix: prefix, open: openSecretManager}, nil
	default:
		return nil, fmt.Errorf("-secrets %q: unknown source; use env, sops:<path> or gcp-secret-manager:<project>", spec)
	}
}

// lookupWith resolves the manifest's placeholders through src and returns
// the lookup the manifest is expanded with. With envFallback, anything src
// does not hold is looked up in the environment: the source wins when both
// have a value; the environment only supplies what the source omits.
// Without it (-require-encrypted-secrets) the environment is never read,
// so a secret exported in a shell cannot slip into an apply unnoticed.
func lookupWith(ctx context.Context, src secretSource, names []string, envFallback bool) (lookupFunc, error) {
	values, err := src.Resolve(ctx, names)
	if err != nil {
		return nil, err
	}
	return func(name string) (string, bool) {
		if v, ok := values[name]; ok {
			return v, true
		}
		if !envFallback {
			return "", false
		}
		return os.LookupEnv(name)
	}, nil
}

// requireEncryptedSource is the -require-encrypted-secrets check on the
// source itself: the values must be encrypted where they are read from.
// For a SOPS file that is a property of each value, not of the file: SOPS
// can leave chosen keys in plaintext (unencrypted_suffix, unencrypted_regex,
// encrypted_regex) and the file still decrypts, so the source is also told
// to verify every requested value is ciphertext in the file as stored.
func requireEncryptedSource(src secretSource) error {
	if !src.Encrypted() {
		return fmt.Errorf("-require-encrypted-secrets: -secrets is %q, which is not encrypted at rest; use sops:<path> or gcp-secret-manager:<project>", src.String())
	}
	if s, ok := src.(*sopsSource); ok {
		s.requireCiphertext = true
	}
	return nil
}

// envSource is the process environment.
type envSource struct{}

func (envSource) Resolve(_ context.Context, names []string) (map[string]string, error) {
	out := make(map[string]string, len(names))
	for _, n := range names {
		if v, ok := os.LookupEnv(n); ok {
			out[n] = v
		}
	}
	return out, nil
}

func (envSource) String() string { return "env" }

func (envSource) Where(name string) string { return "environment variable " + name }

func (envSource) Encrypted() bool { return false }

// sopsSource is a SOPS-encrypted file whose top level maps placeholder
// names to values: YAML or JSON (a flat mapping) or dotenv (KEY=value
// lines). Decryption uses the keys SOPS can reach from this process (GCP
// KMS via application default credentials, AWS KMS, age, PGP); the
// plaintext never touches disk.
type sopsSource struct {
	path string
	// decryptFile is swapped in tests; the default is decrypt.File.
	decryptFile func(path, format string) ([]byte, error)
	// requireCiphertext (set by -require-encrypted-secrets) refuses any
	// requested value that is stored in plaintext in the file, which SOPS
	// allows through unencrypted_suffix / unencrypted_regex /
	// encrypted_regex while the file still decrypts normally.
	requireCiphertext bool
}

func (s *sopsSource) Resolve(_ context.Context, names []string) (map[string]string, error) {
	format, err := sopsFormat(s.path)
	if err != nil {
		return nil, err
	}
	dec := s.decryptFile
	if dec == nil {
		dec = sopsDecrypt
	}
	plain, err := dec(s.path, format)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: %w", s.path, err)
	}
	all, err := parseSecretsFile(plain, format)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.path, err)
	}
	if s.requireCiphertext {
		raw, err := os.ReadFile(s.path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", s.path, err)
		}
		unenc, err := unencryptedKeys(raw, format, names, all)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.path, err)
		}
		if len(unenc) > 0 {
			return nil, fmt.Errorf("-require-encrypted-secrets: %d value(s) are stored in plaintext in %s (left unencrypted by a SOPS unencrypted_suffix, unencrypted_regex or encrypted_regex rule): %s", len(unenc), s.path, strings.Join(unenc, ", "))
		}
	}
	out := make(map[string]string, len(names))
	for _, n := range names {
		if v, ok := all[n]; ok {
			out[n] = v
		}
	}
	return out, nil
}

func (s *sopsSource) String() string { return "sops:" + s.path }

func (s *sopsSource) Where(name string) string { return fmt.Sprintf("key %s in %s", name, s.path) }

func (s *sopsSource) Encrypted() bool { return true }

// sopsDecrypt is the SOPS decryption entry point; a test replaces it to run
// the commands end to end without a key.
var sopsDecrypt = decrypt.File

// unencryptedKeys returns the requested names whose value in the file as
// stored is identical to what decryption yielded. SOPS leaves a value it
// did not encrypt (unencrypted_suffix, unencrypted_regex, encrypted_regex)
// untouched, so "stored == decrypted" is exactly "was never encrypted",
// whatever the value looks like: a literal that happens to start with
// ENC[ is caught the same as any other. An encrypted value never equals
// its ciphertext. The stored file is parsed with the same scalar-to-text
// mapping as the decrypted one, so a number, boolean or null compares by
// its text. Names only are returned: a value found here is exactly what
// must never be printed. A name absent from the file is not reported; it
// is unresolved and reported as such later.
func unencryptedKeys(raw []byte, format string, names []string, decrypted map[string]string) ([]string, error) {
	stored, err := parseSecretsFile(raw, format)
	if err != nil {
		return nil, fmt.Errorf("parse stored content: %w", err)
	}
	var unenc []string
	for _, n := range names {
		if sv, ok := stored[n]; ok {
			if dv, ok := decrypted[n]; ok && sv == dv {
				unenc = append(unenc, n)
			}
		}
	}
	return unenc, nil
}

// sopsFormat maps a file extension to the SOPS input format.
func sopsFormat(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return "yaml", nil
	case ".json":
		return "json", nil
	case ".env":
		return "dotenv", nil
	default:
		return "", fmt.Errorf("%s: unsupported SOPS file type; use .yaml, .yml, .json or .env", path)
	}
}

// parseSecretsFile turns decrypted secrets into name → value. Values must
// be scalars: a nested mapping is a mistake in the file, not something to
// flatten silently.
func parseSecretsFile(plain []byte, format string) (map[string]string, error) {
	out := map[string]string{}
	switch format {
	case "yaml":
		// Decode to nodes and take each scalar's original text, so a numeric
		// secret such as 9007199254740993 is never rounded through a float.
		var raw map[string]yaml.Node
		if err := yaml.Unmarshal(plain, &raw); err != nil {
			return nil, fmt.Errorf("parse decrypted content: %w", err)
		}
		for k, n := range raw {
			if k == "sops" { // metadata block, present when a file was decrypted in place
				continue
			}
			switch {
			case n.Kind == yaml.ScalarNode && n.Tag == "!!null":
				out[k] = ""
			case n.Kind == yaml.ScalarNode:
				out[k] = n.Value
			default:
				return nil, fmt.Errorf("key %q: value must be a scalar", k)
			}
		}
	case "json":
		dec := json.NewDecoder(bytes.NewReader(plain))
		dec.UseNumber() // keep a number's text; float64 would round large ints
		var raw map[string]interface{}
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("parse decrypted content: %w", err)
		}
		for k, v := range raw {
			if k == "sops" {
				continue
			}
			switch val := v.(type) {
			case string:
				out[k] = val
			case json.Number:
				out[k] = val.String()
			case bool:
				out[k] = fmt.Sprint(val)
			case nil:
				out[k] = ""
			default:
				return nil, fmt.Errorf("key %q: value must be a scalar, got %T", k, v)
			}
		}
	case "dotenv":
		for i, line := range strings.Split(string(plain), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				// The line is decrypted secret material; name it by number only.
				return nil, fmt.Errorf("dotenv line %d: expected KEY=value", i+1)
			}
			if k = strings.TrimSpace(k); k != "sops_version" && !strings.HasPrefix(k, "sops_") {
				out[k] = strings.Trim(strings.TrimSpace(v), `"'`)
			}
		}
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
	return out, nil
}

// secretAccessor is the slice of Secret Manager the gcp source needs, so the
// lookup logic is tested against a fake and the real client is only
// exercised with credentials.
type secretAccessor interface {
	AccessSecretVersion(ctx context.Context, req *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error)
	Close() error
}

func openSecretManager(ctx context.Context) (secretAccessor, error) {
	c, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return gapiAccessor{c}, nil
}

// gapiAccessor adapts the generated client, whose methods take variadic
// call options, to secretAccessor.
type gapiAccessor struct{ c *secretmanager.Client }

func (g gapiAccessor) AccessSecretVersion(ctx context.Context, req *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	return g.c.AccessSecretVersion(ctx, req)
}

func (g gapiAccessor) Close() error { return g.c.Close() }

// gcpSource reads the latest version of one secret per placeholder from
// Google Secret Manager. The secret id is derived from the placeholder:
// drop a leading NEXUS_PROVIDER_, lower-case, '_' → '-', then prepend
// -secret-prefix. So ${NEXUS_PROVIDER_TWITTER_CLIENT_ID} with prefix
// "oauth-" reads oauth-twitter-client-id, and ${GITHUB_CLIENT_SECRET} with
// no prefix reads github-client-secret. Authentication is application
// default credentials.
type gcpSource struct {
	project string
	prefix  string
	open    func(ctx context.Context) (secretAccessor, error)
}

// gcpConcurrency bounds in-flight reads; a manifest of 150 providers is 300
// secrets, and one call each in sequence is minutes.
const gcpConcurrency = 16

func (g *gcpSource) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	if len(names) == 0 {
		return map[string]string{}, nil
	}
	client, err := g.open(ctx)
	if err != nil {
		return nil, fmt.Errorf("secret manager client: %w", err)
	}
	defer client.Close()

	out := make(map[string]string, len(names))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, gcpConcurrency)
	var firstErr error
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			id := secretIDFor(name, g.prefix)
			resp, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
				Name: fmt.Sprintf("projects/%s/secrets/%s/versions/latest", g.project, id),
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if status.Code(err) == codes.NotFound {
					return // unresolved: reported by name with the others
				}
				if firstErr == nil {
					firstErr = fmt.Errorf("read secret %s (for ${%s}): %w", id, name, err)
				}
				return
			}
			out[name] = string(resp.GetPayload().GetData())
		}(name)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func (g *gcpSource) String() string { return "gcp-secret-manager:" + g.project }

func (g *gcpSource) Where(name string) string {
	return fmt.Sprintf("secret %s in project %s", secretIDFor(name, g.prefix), g.project)
}

func (g *gcpSource) Encrypted() bool { return true }

// unresolvedMessage says, for each missing placeholder, exactly what was
// looked for and where, so the fix is obvious from the message alone.
// envFallback says whether the environment was tried after the source.
func unresolvedMessage(src secretSource, missing []string, envFallback bool) string {
	lines := make([]string, len(missing))
	for i, name := range missing {
		where := src.Where(name)
		if envFallback && src.String() != "env" {
			where += ", then environment variable " + name
		}
		lines[i] = fmt.Sprintf("  ${%s}: not found (looked for %s)", name, where)
	}
	return fmt.Sprintf("%d placeholder(s) could not be resolved from %s:\n%s", len(missing), src, strings.Join(lines, "\n"))
}

// secretIDFor derives the Secret Manager secret id for a placeholder.
func secretIDFor(placeholder, prefix string) string {
	name := strings.TrimPrefix(placeholder, "NEXUS_PROVIDER_")
	return prefix + strings.ReplaceAll(strings.ToLower(name), "_", "-")
}
