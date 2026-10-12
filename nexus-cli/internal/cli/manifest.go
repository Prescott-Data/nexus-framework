package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Provider is one entry of the manifest; its JSON form is what the Broker's
// POST /providers "profile" and PATCH /providers/{id} accept.
type Provider struct {
	Name string `yaml:"name" json:"name"`
	// Pointers: an omitted field is left as the Broker has it, while an
	// explicit empty string ("") is sent and clears the live value.
	Description      *string                `yaml:"description,omitempty" json:"description,omitempty"`
	Category         *string                `yaml:"category,omitempty" json:"category,omitempty"`
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
	// Sensitive[i][field] is true when provider i's top-level field held a
	// placeholder anywhere in its subtree, so plan output masks it: a
	// resolved value is a secret even when the field is params.
	Sensitive []map[string]bool
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
	doc, err := parseSingleDocument(data)
	if err != nil {
		return loaded{}, fmt.Errorf("parse %s: %w", path, err)
	}
	items, err := manifestProviders(doc)
	if err != nil {
		return loaded{}, fmt.Errorf("%s: %w", path, err)
	}
	for i, item := range items {
		if err := checkProviderKeys(item); err != nil {
			return loaded{}, fmt.Errorf("%s: providers[%d] (line %d): %w", path, i, item.Line, err)
		}
	}
	l, err := decodeDocument(doc, lookup)
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
		file, err := parseSingleDocument(data)
		if err != nil {
			return loaded{}, fmt.Errorf("parse %s: %w", path, err)
		}
		if len(file.Content) == 0 || file.Content[0].Kind != yaml.MappingNode {
			return loaded{}, fmt.Errorf("%s: expected a single provider mapping with a name", path)
		}
		mapping := file.Content[0]
		if scalarValue(mapping, "name") == "" && mappingHas(mapping, "providers") {
			return loaded{}, fmt.Errorf("%s: holds a providers list; a provider directory wants one provider mapping per file", path)
		}
		name := scalarValue(mapping, "name")
		if name == "" {
			return loaded{}, fmt.Errorf("%s: expected a single provider mapping with a name", path)
		}
		if err := checkProviderKeys(mapping); err != nil {
			return loaded{}, fmt.Errorf("%s: %w", path, err)
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

// parseSingleDocument parses exactly one YAML document. A second document
// (after a `---`) is an error: the first would otherwise be used and the
// rest dropped without a word.
func parseSingleDocument(data []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return &yaml.Node{Kind: yaml.DocumentNode}, nil // empty file
		}
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("contains more than one YAML document; expected exactly one")
	}
	return &doc, nil
}

// manifestProviders checks that a single-file manifest is a mapping whose
// only key is `providers`, holding a sequence, and returns the sequence
// items. Anything else (a typo such as `provider:`, an empty file, a
// provider mapping passed as a manifest) would otherwise decode to zero
// providers, which `apply -prune` would act on by deleting every live
// provider. An explicit `providers: []` is accepted as a deliberate empty
// state.
func manifestProviders(doc *yaml.Node) ([]*yaml.Node, error) {
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("manifest must be a mapping with a top-level providers list")
	}
	root := doc.Content[0]
	if scalarValue(root, "name") != "" && !mappingHas(root, "providers") {
		return nil, errors.New("this is a single provider file, not a manifest; pass its directory with -file, or wrap it in a providers list")
	}
	var seq *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if k.Value != "providers" {
			return nil, fmt.Errorf("unknown top-level key %q (line %d); a manifest has only providers", k.Value, k.Line)
		}
		seq = v
	}
	if seq == nil {
		return nil, errors.New("manifest has no providers key")
	}
	if seq.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("providers (line %d) must be a list", seq.Line)
	}
	for i, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("providers[%d] (line %d) must be a mapping", i, item.Line)
		}
	}
	return seq.Content, nil
}

// providerKeys is every yaml key Provider accepts, from its struct tags, so
// the list cannot drift from the type.
var providerKeys = func() map[string]bool {
	keys := map[string]bool{}
	t := reflect.TypeOf(Provider{})
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("yaml")
		if name, _, _ := strings.Cut(tag, ","); name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
}()

// checkProviderKeys rejects a key Provider does not have. Decoding would
// drop it silently, so `scope:` for `scopes:` or `cleint_id` would register
// a provider without the field and nothing would say so.
func checkProviderKeys(mapping *yaml.Node) error {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k := mapping.Content[i]
		if k.Kind == yaml.ScalarNode && !providerKeys[k.Value] {
			return fmt.Errorf("unknown field %q (line %d)", k.Value, k.Line)
		}
	}
	return nil
}

// mappingHas reports whether key is present in a mapping node.
func mappingHas(mapping *yaml.Node, key string) bool {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if k := mapping.Content[i]; k.Kind == yaml.ScalarNode && k.Value == key {
			return true
		}
	}
	return false
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

// placeholderFields records, per provider, which top-level fields contain a
// placeholder somewhere beneath them, before expansion replaces it.
func placeholderFields(doc *yaml.Node) []map[string]bool {
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	var seq *yaml.Node
	for i := 0; i+1 < len(doc.Content[0].Content); i += 2 {
		if doc.Content[0].Content[i].Value == "providers" {
			seq = doc.Content[0].Content[i+1]
		}
	}
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]map[string]bool, len(seq.Content))
	for i, item := range seq.Content {
		out[i] = map[string]bool{}
		if item.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(item.Content); j += 2 {
			key, value := item.Content[j].Value, item.Content[j+1]
			walkValues(value, func(n *yaml.Node) {
				if len(placeholdersIn(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{n}})) > 0 {
					out[i][key] = true
				}
			})
		}
	}
	return out
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
	l := loaded{Placeholders: placeholdersIn(doc), Sensitive: placeholderFields(doc)}
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
	doc, err := parseSingleDocument(data)
	if err != nil {
		return Manifest{}, nil, err
	}
	l, err := decodeDocument(doc, lookup)
	if err != nil {
		return Manifest{}, l.Missing, err
	}
	return l.Manifest, l.Missing, nil
}
