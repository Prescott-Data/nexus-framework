package cli

import (
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

// lookupWith resolves the manifest's placeholders through src, then falls
// back to the environment for anything src does not hold, and returns the
// lookup the manifest is expanded with. The source wins when both have a
// value; the environment only supplies what the source omits.
func lookupWith(ctx context.Context, src secretSource, names []string) (lookupFunc, error) {
	values, err := src.Resolve(ctx, names)
	if err != nil {
		return nil, err
	}
	return func(name string) (string, bool) {
		if v, ok := values[name]; ok {
			return v, true
		}
		return os.LookupEnv(name)
	}, nil
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

// sopsSource is a SOPS-encrypted file whose top level maps placeholder
// names to values: YAML or JSON (a flat mapping) or dotenv (KEY=value
// lines). Decryption uses the keys SOPS can reach from this process (GCP
// KMS via application default credentials, AWS KMS, age, PGP); the
// plaintext never touches disk.
type sopsSource struct {
	path string
	// decryptFile is swapped in tests; the default is decrypt.File.
	decryptFile func(path, format string) ([]byte, error)
}

func (s *sopsSource) Resolve(_ context.Context, names []string) (map[string]string, error) {
	format, err := sopsFormat(s.path)
	if err != nil {
		return nil, err
	}
	dec := s.decryptFile
	if dec == nil {
		dec = decrypt.File
	}
	plain, err := dec(s.path, format)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: %w", s.path, err)
	}
	all, err := parseSecretsFile(plain, format)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.path, err)
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
	case "yaml", "json":
		var raw map[string]interface{}
		if format == "yaml" {
			if err := yaml.Unmarshal(plain, &raw); err != nil {
				return nil, fmt.Errorf("parse decrypted content: %w", err)
			}
		} else if err := json.Unmarshal(plain, &raw); err != nil {
			return nil, fmt.Errorf("parse decrypted content: %w", err)
		}
		for k, v := range raw {
			if k == "sops" { // metadata block, present when a file was decrypted in place
				continue
			}
			switch val := v.(type) {
			case string:
				out[k] = val
			case bool, int, int64, float64:
				out[k] = fmt.Sprint(val)
			case nil:
				out[k] = ""
			default:
				return nil, fmt.Errorf("key %q: value must be a scalar, got %T", k, v)
			}
		}
	case "dotenv":
		for _, line := range strings.Split(string(plain), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				return nil, fmt.Errorf("dotenv line %q: expected KEY=value", line)
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

// unresolvedMessage says, for each missing placeholder, exactly what was
// looked for and where, so the fix is obvious from the message alone.
func unresolvedMessage(src secretSource, missing []string) string {
	lines := make([]string, len(missing))
	for i, name := range missing {
		where := src.Where(name)
		if src.String() != "env" {
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
