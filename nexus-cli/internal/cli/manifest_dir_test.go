package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const githubFile = `# ${IN_A_COMMENT} is not a placeholder
name: github
auth_type: oauth2
client_id: "${NEXUS_PROVIDER_GITHUB_CLIENT_ID}"
client_secret: "${NEXUS_PROVIDER_GITHUB_CLIENT_SECRET}"
auth_url: https://github.com/login/oauth/authorize
token_url: https://github.com/login/oauth/access_token
scopes: [read:user]
`

const slackFile = `name: slack
auth_type: oauth2
client_id: "${NEXUS_PROVIDER_SLACK_CLIENT_ID}"
client_secret: "${NEXUS_PROVIDER_SLACK_CLIENT_SECRET}"
auth_url: https://slack.com/oauth/v2/authorize
token_url: https://slack.com/api/oauth.v2.access
scopes: []
`

func TestReadManifest_DirectoryAssemblesSortedAndAttributesSources(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"slack.yaml":  slackFile,
		"github.yaml": githubFile,
		"README.md":   "not a provider",
		".draft.yaml": "name: draft\n", // hidden files are skipped
		"notes.txt":   "name: notes\n",
	})
	env := map[string]string{
		"NEXUS_PROVIDER_GITHUB_CLIENT_ID": "gid", "NEXUS_PROVIDER_GITHUB_CLIENT_SECRET": "gsec",
		"NEXUS_PROVIDER_SLACK_CLIENT_ID": "sid", "NEXUS_PROVIDER_SLACK_CLIENT_SECRET": "ssec",
	}
	l, err := readManifest(dir, func(n string) (string, bool) { v, ok := env[n]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if l.Files != 2 || len(l.Manifest.Providers) != 2 {
		t.Fatalf("files=%d providers=%d, want 2 and 2", l.Files, len(l.Manifest.Providers))
	}
	if l.Manifest.Providers[0].Name != "github" || l.Manifest.Providers[1].Name != "slack" {
		t.Fatalf("order = %s, %s; want github, slack (file name order)", l.Manifest.Providers[0].Name, l.Manifest.Providers[1].Name)
	}
	if l.Manifest.Providers[0].ClientID != "gid" || l.Manifest.Providers[1].ClientSecret != "ssec" {
		t.Fatalf("placeholders not expanded: %+v", l.Manifest.Providers)
	}
	if filepath.Base(l.Sources[1]) != "slack.yaml" {
		t.Fatalf("Sources[1] = %s, want slack.yaml", l.Sources[1])
	}
	want := []string{"NEXUS_PROVIDER_GITHUB_CLIENT_ID", "NEXUS_PROVIDER_GITHUB_CLIENT_SECRET", "NEXUS_PROVIDER_SLACK_CLIENT_ID", "NEXUS_PROVIDER_SLACK_CLIENT_SECRET"}
	if strings.Join(l.Placeholders, ",") != strings.Join(want, ",") {
		t.Fatalf("Placeholders = %v, want %v (the comment must not count)", l.Placeholders, want)
	}
	if len(l.Missing) != 0 {
		t.Fatalf("Missing = %v, want none", l.Missing)
	}
}

func TestReadManifest_DirectoryRejectsNameMismatch(t *testing.T) {
	dir := writeFiles(t, map[string]string{"gh.yaml": githubFile})
	_, err := readManifest(dir, keepPlaceholders)
	if err == nil || !strings.Contains(err.Error(), `provider name "github" does not match the file name`) {
		t.Fatalf("err = %v, want a name mismatch error", err)
	}
}

func TestReadManifest_DirectoryRejectsDuplicateName(t *testing.T) {
	// Same provider under .yaml and .yml: both stems are "github".
	dir := writeFiles(t, map[string]string{"github.yaml": githubFile, "github.yml": githubFile})
	_, err := readManifest(dir, keepPlaceholders)
	if err == nil || !strings.Contains(err.Error(), `provider "github" is defined in both github.yaml and github.yml`) {
		t.Fatalf("err = %v, want a duplicate error", err)
	}
}

func TestReadManifest_DirectoryRejectsListFile(t *testing.T) {
	dir := writeFiles(t, map[string]string{"github.yaml": "providers:\n  - name: github\n"})
	_, err := readManifest(dir, keepPlaceholders)
	if err == nil || !strings.Contains(err.Error(), "holds a providers list; a provider directory wants one provider mapping per file") {
		t.Fatalf("err = %v, want a providers-list error", err)
	}
}

func TestReadManifest_RejectsUnknownFieldWithLine(t *testing.T) {
	dir := writeFiles(t, map[string]string{"github.yaml": "name: github\nauth_type: oauth2\nscope: [a]\n"})
	_, err := readManifest(dir, keepPlaceholders)
	if err == nil || !strings.Contains(err.Error(), `github.yaml: unknown field "scope" (line 3)`) {
		t.Fatalf("dir: err = %v, want unknown field with line", err)
	}
	single := writeFiles(t, map[string]string{"m.yaml": "providers:\n  - name: github\n    cleint_id: x\n"})
	_, err = readManifest(filepath.Join(single, "m.yaml"), keepPlaceholders)
	if err == nil || !strings.Contains(err.Error(), `providers[0] (line 2): unknown field "cleint_id" (line 3)`) {
		t.Fatalf("file: err = %v, want unknown field with line", err)
	}
}

func TestReadManifest_RejectsMultipleDocuments(t *testing.T) {
	dir := writeFiles(t, map[string]string{"github.yaml": githubFile + "---\nname: other\n"})
	if _, err := readManifest(dir, keepPlaceholders); err == nil || !strings.Contains(err.Error(), "more than one YAML document") {
		t.Fatalf("err = %v, want multi-document error", err)
	}
}

func TestReadManifest_SingleFileRootMustBeProvidersList(t *testing.T) {
	cases := map[string]string{
		"provider:\n  - name: a\n":          `unknown top-level key "provider" (line 1)`,
		"":                                  "manifest must be a mapping with a top-level providers list",
		"{}":                                "manifest has no providers key",
		"providers: {name: a}\n":            "providers (line 1) must be a list",
		"providers:\n  - just-a-string\n":   "providers[0] (line 2) must be a mapping",
		"name: github\nauth_type: oauth2\n": "this is a single provider file, not a manifest",
		"providers: []\nextra: 1\n":         `unknown top-level key "extra" (line 2)`,
	}
	for doc, want := range cases {
		dir := writeFiles(t, map[string]string{"m.yaml": doc})
		_, err := readManifest(filepath.Join(dir, "m.yaml"), keepPlaceholders)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", doc, err, want)
		}
	}
	// An explicit empty list is a deliberate empty state.
	dir := writeFiles(t, map[string]string{"m.yaml": "providers: []\n"})
	l, err := readManifest(filepath.Join(dir, "m.yaml"), keepPlaceholders)
	if err != nil || len(l.Manifest.Providers) != 0 {
		t.Fatalf("providers: [] should load as zero providers, got %v %v", l.Manifest.Providers, err)
	}
}

func TestReadManifest_DirectoryWithNoYAMLFails(t *testing.T) {
	dir := writeFiles(t, map[string]string{"README.md": "x"})
	if _, err := readManifest(dir, keepPlaceholders); err == nil || !strings.Contains(err.Error(), "no .yaml files") {
		t.Fatalf("err = %v, want no .yaml files", err)
	}
}

func TestReadManifest_SingleFileKeepsPlaceholdersWhenAsked(t *testing.T) {
	dir := writeFiles(t, map[string]string{"m.yaml": "providers:\n  - name: github\n    client_id: ${A}\n    client_secret: \"${B}\"\n"})
	l, err := readManifest(filepath.Join(dir, "m.yaml"), keepPlaceholders)
	if err != nil {
		t.Fatal(err)
	}
	p := l.Manifest.Providers[0]
	if p.ClientID != "${A}" || p.ClientSecret != "${B}" {
		t.Fatalf("keepPlaceholders must leave values in place, got %+v", p)
	}
	if l.Files != 1 || len(l.Missing) != 0 {
		t.Fatalf("files=%d missing=%v", l.Files, l.Missing)
	}
}

func TestReadManifest_MissingPathIsAnError(t *testing.T) {
	if _, err := readManifest(filepath.Join(t.TempDir(), "nope.yaml"), keepPlaceholders); err == nil {
		t.Fatal("want an error for a missing manifest")
	}
}
