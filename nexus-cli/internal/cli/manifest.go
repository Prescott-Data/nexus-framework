package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Provider is one entry of the manifest; its JSON form is what the Broker's
// POST /providers "profile" and PATCH /providers/{id} accept.
type Provider struct {
	Name             string                 `yaml:"name" json:"name"`
	AuthType         string                 `yaml:"auth_type,omitempty" json:"auth_type,omitempty"`
	AuthHeader       string                 `yaml:"auth_header,omitempty" json:"auth_header,omitempty"`
	ClientID         string                 `yaml:"client_id,omitempty" json:"client_id,omitempty"`
	ClientSecret     string                 `yaml:"client_secret,omitempty" json:"client_secret,omitempty"`
	AuthURL          string                 `yaml:"auth_url,omitempty" json:"auth_url,omitempty"`
	TokenURL         string                 `yaml:"token_url,omitempty" json:"token_url,omitempty"`
	Issuer           string                 `yaml:"issuer,omitempty" json:"issuer,omitempty"`
	EnableDiscovery  bool                   `yaml:"enable_discovery" json:"enable_discovery"`
	Scopes           []string               `yaml:"scopes" json:"scopes"`
	APIBaseURL       string                 `yaml:"api_base_url,omitempty" json:"api_base_url,omitempty"`
	UserInfoEndpoint string                 `yaml:"user_info_endpoint,omitempty" json:"user_info_endpoint,omitempty"`
	Params           map[string]interface{} `yaml:"params,omitempty" json:"params,omitempty"`

	// SAML providers (auth_type: saml); the Broker requires all four.
	SAMLIdpEntityID string `yaml:"saml_idp_entity_id,omitempty" json:"saml_idp_entity_id,omitempty"`
	SAMLIdpSSOURL   string `yaml:"saml_idp_sso_url,omitempty" json:"saml_idp_sso_url,omitempty"`
	SAMLIdpX509Cert string `yaml:"saml_idp_x509_cert,omitempty" json:"saml_idp_x509_cert,omitempty"`
	SAMLSPEntityID  string `yaml:"saml_sp_entity_id,omitempty" json:"saml_sp_entity_id,omitempty"`
}

// Manifest is the desired state: every provider the Broker should have.
type Manifest struct {
	Providers []Provider `yaml:"providers"`
}

// loaded is a manifest read from disk, with where each provider came from
// (for messages) and which placeholders it uses.
type loaded struct {
	Manifest     Manifest
	Sources      []string // Sources[i] is the file Providers[i] was read from
	Files        int      // number of files read (1 for a single manifest)
	Directory    bool     // read from a provider directory rather than one manifest
	Placeholders []string // every ${NAME} referenced, in document order, unique
	Missing      []string // placeholders the lookup could not resolve
}

// lookupFunc resolves a placeholder name; ok=false means unresolved.
type lookupFunc func(name string) (string, bool)

// keepPlaceholders is a lookup that leaves every ${NAME} in place, for
// checks that must not depend on secrets being available.
func keepPlaceholders(name string) (string, bool) { return "${" + name + "}", true }

// readManifest reads a manifest file or a provider directory and expands
// placeholders with lookup. In directory mode each *.yaml / *.yml file must
// be a single provider mapping whose name equals the file's stem, and a
// name may appear only once; those are hard errors, since a copy-paste slip
// should fail here rather than reach the Broker.
func readManifest(path string, lookup lookupFunc) (loaded, error) {
	info, err := os.Stat(path)
	if err != nil {
		return loaded{}, fmt.Errorf("read manifest %s: %w", path, err)
	}
	if info.IsDir() {
		return readProviderDir(path, lookup)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return loaded{}, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return loaded{}, fmt.Errorf("parse %s: %w", path, err)
	}
	l, err := decodeDocument(&doc, lookup)
	if err != nil {
		return loaded{}, fmt.Errorf("parse %s: %w", path, err)
	}
	l.Files = 1
	l.Sources = make([]string, len(l.Manifest.Providers))
	for i := range l.Sources {
		l.Sources[i] = path
	}
	return l, nil
}

// readProviderDir assembles <dir>/<name>.yaml files into one manifest, the
// way Terraform treats every .tf file in a directory as one configuration.
func readProviderDir(dir string, lookup lookupFunc) (loaded, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return loaded{}, fmt.Errorf("read provider directory %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasPrefix(n, ".") {
			continue
		}
		if ext := filepath.Ext(n); ext == ".yaml" || ext == ".yml" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return loaded{}, fmt.Errorf("read provider directory %s: no .yaml files", dir)
	}

	// One document holding a providers sequence, so expansion and decoding
	// run exactly as they do for a single manifest file.
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{
		Kind: yaml.MappingNode, Tag: "!!map",
		Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "providers"}, seq},
	}}}
	var sources []string
	definedIn := map[string]string{}
	for _, n := range names {
		path := filepath.Join(dir, n)
		data, err := os.ReadFile(path)
		if err != nil {
			return loaded{}, fmt.Errorf("read %s: %w", path, err)
		}
		var file yaml.Node
		if err := yaml.Unmarshal(data, &file); err != nil {
			return loaded{}, fmt.Errorf("parse %s: %w", path, err)
		}
		if len(file.Content) == 0 || file.Content[0].Kind != yaml.MappingNode {
			return loaded{}, fmt.Errorf("%s: expected a single provider mapping with a name", path)
		}
		mapping := file.Content[0]
		name := scalarValue(mapping, "name")
		if name == "" {
			return loaded{}, fmt.Errorf("%s: expected a single provider mapping with a name", path)
		}
		if stem := strings.TrimSuffix(n, filepath.Ext(n)); name != stem {
			return loaded{}, fmt.Errorf("%s: provider name %q does not match the file name", path, name)
		}
		if prev, dup := definedIn[name]; dup {
			return loaded{}, fmt.Errorf("provider %q is defined in both %s and %s", name, prev, n)
		}
		definedIn[name] = n
		seq.Content = append(seq.Content, mapping)
		sources = append(sources, path)
	}
	l, err := decodeDocument(doc, lookup)
	if err != nil {
		return loaded{}, fmt.Errorf("parse provider directory %s: %w", dir, err)
	}
	l.Files = len(names)
	l.Directory = true
	l.Sources = sources
	return l, nil
}

// scalarValue returns the scalar value of key in a mapping node, or "".
func scalarValue(mapping *yaml.Node, key string) string {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k, v := mapping.Content[i], mapping.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key && v.Kind == yaml.ScalarNode {
			return v.Value
		}
	}
	return ""
}

// placeholdersIn lists every ${NAME} / $NAME referenced in value scalars of
// the document, unique, in document order. Keys and comments are not values
// and are never scanned.
func placeholdersIn(doc *yaml.Node) []string {
	var names []string
	seen := map[string]bool{}
	walkValues(doc, func(n *yaml.Node) {
		if !strings.Contains(n.Value, "$") {
			return
		}
		os.Expand(n.Value, func(name string) string {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
			return ""
		})
	})
	return names
}

// walkValues calls fn on every value scalar in the tree: sequence items and
// mapping values, never mapping keys.
func walkValues(n *yaml.Node, fn func(*yaml.Node)) {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			walkValues(c, fn)
		}
	case yaml.MappingNode:
		for i := 1; i < len(n.Content); i += 2 { // values only; even indexes are keys
			walkValues(n.Content[i], fn)
		}
	case yaml.ScalarNode:
		fn(n)
	}
}

// decodeDocument expands placeholders in value scalars on the node tree and
// decodes the result. A plain (unquoted) scalar is re-typed after expansion,
// so `skip_scope_on_exchange: ${SKIP}` with SKIP=true still decodes to a
// boolean; a quoted or tagged scalar stays a string.
func decodeDocument(doc *yaml.Node, lookup lookupFunc) (loaded, error) {
	l := loaded{Placeholders: placeholdersIn(doc)}
	seen := map[string]bool{}
	walkValues(doc, func(n *yaml.Node) {
		if !strings.Contains(n.Value, "$") {
			return
		}
		n.Value = os.Expand(n.Value, func(name string) string {
			val, ok := lookup(name)
			if !ok && !seen[name] {
				seen[name] = true
				l.Missing = append(l.Missing, name)
			}
			return val
		})
		if n.Style == 0 {
			// Untagged plain scalar: let the decoder infer bool/int/string
			// again. An explicit tag (!!str) or any quoting is the author's
			// choice and is kept.
			n.Tag = ""
		}
	})
	if len(doc.Content) > 0 {
		if err := doc.Decode(&l.Manifest); err != nil {
			return loaded{}, err
		}
	}
	return l, nil
}

// loadManifest parses manifest bytes and expands placeholders. Kept as the
// unit under test for expansion; readManifest is what the commands use.
func loadManifest(data []byte, lookup lookupFunc) (Manifest, []string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Manifest{}, nil, err
	}
	l, err := decodeDocument(&doc, lookup)
	if err != nil {
		return Manifest{}, l.Missing, err
	}
	return l.Manifest, l.Missing, nil
}
