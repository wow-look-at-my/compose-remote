package compose

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LabelHash is the label compose-remote injects on every service's
// containers. The value is our deterministic config hash for that service;
// it is what we read back via `docker inspect` to decide whether a
// container's config matches the desired file.
const LabelHash = "io.compose-remote.config-hash"

// Service is the parsed (desired) config for a single compose service.
type Service struct {
	Name string
	// Hash is the deterministic config hash we compute. Stored as
	// LabelHash on the rendered service so we can compare actual to
	// desired without trusting docker compose's own hashing.
	Hash string
	// Image is the image reference from the service.
	Image string
}

// Parsed represents a compose file after we've augmented it with our
// per-service hash labels. Render it with Marshal.
type Parsed struct {
	doc      yaml.Node // the root document node
	services map[string]Service
	dir      string // the directory containing the original file (for env_file paths)
}

// Parse parses a compose file's bytes into a Parsed value with per-service
// hashes computed and the LabelHash injected onto each service's `labels:`.
//
// `dir` is the directory the original file lived in, used to resolve
// relative paths inside `env_file:` entries when computing hashes.
func Parse(content []byte, dir string) (*Parsed, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, fmt.Errorf("parse compose yaml: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, fmt.Errorf("parse compose yaml: empty document")
	}
	top := root.Content[0]
	if top.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("parse compose yaml: top-level is not a mapping")
	}
	servicesNode := mapValue(top, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("parse compose yaml: missing services: section")
	}

	parsed := &Parsed{doc: root, services: map[string]Service{}, dir: dir}
	for i := 0; i < len(servicesNode.Content); i += 2 {
		nameNode := servicesNode.Content[i]
		svcNode := servicesNode.Content[i+1]
		if svcNode.Kind != yaml.MappingNode {
			continue
		}
		name := nameNode.Value
		hash, image, err := serviceHash(svcNode, dir)
		if err != nil {
			return nil, fmt.Errorf("hash service %s: %w", name, err)
		}
		injectLabel(svcNode, LabelHash, hash)
		parsed.services[name] = Service{Name: name, Hash: hash, Image: image}
	}
	return parsed, nil
}

// Services returns the set of services indexed by name.
func (p *Parsed) Services() map[string]Service { return p.services }

// Marshal returns the YAML bytes after label injection.
func (p *Parsed) Marshal() ([]byte, error) {
	out, err := yaml.Marshal(&p.doc)
	if err != nil {
		return nil, fmt.Errorf("marshal compose yaml: %w", err)
	}
	return out, nil
}

// mapValue returns the value node for a key in a YAML mapping, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// injectLabel ensures svc.labels[key] = value, creating the labels map
// if absent. Handles both the map-form and list-form of compose labels.
func injectLabel(svc *yaml.Node, key, value string) {
	labels := mapValue(svc, "labels")
	if labels == nil {
		// Add a new mapping: labels: { <key>: <value> }
		svc.Content = append(svc.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "labels"},
			&yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
				},
			},
		)
		return
	}
	switch labels.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(labels.Content); i += 2 {
			if labels.Content[i].Value == key {
				labels.Content[i+1].Tag = "!!str"
				labels.Content[i+1].Value = value
				return
			}
		}
		labels.Content = append(labels.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
		)
	case yaml.SequenceNode:
		// list form: ["k=v", ...]
		for _, item := range labels.Content {
			if len(item.Value) > len(key)+1 && item.Value[:len(key)+1] == key+"=" {
				item.Value = key + "=" + value
				return
			}
		}
		labels.Content = append(labels.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key + "=" + value},
		)
	}
}

// serviceHash computes a deterministic hash of a single service's effective
// config. It includes the canonical JSON of the service node AND the
// content of any referenced env_file paths.
//
// The hash MUST be stable across round-trips through Parse + Marshal: if
// we parse a file we previously rendered (which has LabelHash injected),
// the hash should match the previously-injected value. We achieve that by
// stripping our own label before canonicalizing.
func serviceHash(svc *yaml.Node, dir string) (hash, image string, err error) {
	canon, err := canonicalize(stripOwnLabel(svc))
	if err != nil {
		return "", "", err
	}
	if m, ok := canon.(map[string]any); ok {
		if v, ok := m["image"].(string); ok {
			image = v
		}
	}
	js, err := json.Marshal(canon)
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	h.Write(js)

	for _, p := range envFilePaths(svc) {
		path := p
		if !filepath.IsAbs(path) && dir != "" {
			path = filepath.Join(dir, path)
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			h.Write([]byte("\x00env_file_missing:" + p + "\x00"))
			continue
		}
		h.Write([]byte("\x00env_file:" + p + "\x00"))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), image, nil
}

// stripOwnLabel returns a deep-ish copy of the service node with our
// LabelHash entry removed from labels. It only copies the bits it needs
// to mutate, leaving the rest aliased — that's fine because canonicalize
// only reads.
func stripOwnLabel(svc *yaml.Node) *yaml.Node {
	if svc == nil || svc.Kind != yaml.MappingNode {
		return svc
	}
	labels := mapValue(svc, "labels")
	if labels == nil {
		return svc
	}
	switch labels.Kind {
	case yaml.MappingNode:
		hasOurs := false
		for i := 0; i < len(labels.Content); i += 2 {
			if labels.Content[i].Value == LabelHash {
				hasOurs = true
				break
			}
		}
		if !hasOurs {
			return svc
		}
		newLabels := &yaml.Node{Kind: yaml.MappingNode}
		for i := 0; i < len(labels.Content); i += 2 {
			if labels.Content[i].Value == LabelHash {
				continue
			}
			newLabels.Content = append(newLabels.Content,
				labels.Content[i], labels.Content[i+1])
		}
		return rebuildSvc(svc, newLabels)
	case yaml.SequenceNode:
		hasOurs := false
		for _, item := range labels.Content {
			if strings.HasPrefix(item.Value, LabelHash+"=") {
				hasOurs = true
				break
			}
		}
		if !hasOurs {
			return svc
		}
		newLabels := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range labels.Content {
			if strings.HasPrefix(item.Value, LabelHash+"=") {
				continue
			}
			newLabels.Content = append(newLabels.Content, item)
		}
		return rebuildSvc(svc, newLabels)
	}
	return svc
}

// rebuildSvc returns a new mapping node identical to svc except its
// `labels:` value is replaced with newLabels. If newLabels is empty
// (no other entries), the labels key is dropped entirely so that adding
// vs. not adding a stray empty labels: doesn't perturb the hash.
func rebuildSvc(svc, newLabels *yaml.Node) *yaml.Node {
	out := &yaml.Node{Kind: yaml.MappingNode}
	for i := 0; i < len(svc.Content); i += 2 {
		k := svc.Content[i]
		v := svc.Content[i+1]
		if k.Value == "labels" {
			if len(newLabels.Content) == 0 {
				continue
			}
			out.Content = append(out.Content, k, newLabels)
			continue
		}
		out.Content = append(out.Content, k, v)
	}
	return out
}

// envFilePaths returns the env_file paths declared on a service. Compose
// supports both `env_file: foo.env` and `env_file: [a.env, b.env]`.
func envFilePaths(svc *yaml.Node) []string {
	n := mapValue(svc, "env_file")
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return []string{n.Value}
	case yaml.SequenceNode:
		out := make([]string, 0, len(n.Content))
		for _, item := range n.Content {
			switch item.Kind {
			case yaml.ScalarNode:
				out = append(out, item.Value)
			case yaml.MappingNode:
				if v := mapValue(item, "path"); v != nil && v.Kind == yaml.ScalarNode {
					out = append(out, v.Value)
				}
			}
		}
		return out
	}
	return nil
}

// canonicalize converts a yaml.Node into a Go value with all maps as
// sorted-key map[string]any. The result is JSON-marshallable to a
// deterministic byte sequence.
func canonicalize(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return canonicalize(n.Content[0])
	case yaml.MappingNode:
		out := map[string]any{}
		keys := make([]string, 0, len(n.Content)/2)
		tmp := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i].Value
			v, err := canonicalize(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			tmp[k] = v
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// re-insert in sorted order; Go maps are unordered but
		// json.Marshal sorts keys alphabetically already so this is
		// just for clarity.
		for _, k := range keys {
			out[k] = tmp[k]
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := canonicalize(c)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.ScalarNode:
		// Always treat scalars as strings to avoid
		// "1.0" vs "1" type-coercion churn between yaml versions.
		return n.Value, nil
	case yaml.AliasNode:
		if n.Alias != nil {
			return canonicalize(n.Alias)
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unknown yaml node kind: %d", n.Kind)
}
