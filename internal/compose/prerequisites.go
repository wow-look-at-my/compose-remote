package compose

import (
	"context"
	"strings"

	"gopkg.in/yaml.v3"
)

// ExternalNetworks returns the names of all top-level networks declared
// with external: true.
func (p *Parsed) ExternalNetworks() []string {
	return externalResourceNames(&p.doc, "networks")
}

// ExternalVolumes returns the names of all top-level volumes declared
// with external: true.
func (p *Parsed) ExternalVolumes() []string {
	return externalResourceNames(&p.doc, "volumes")
}

// externalResourceNames reads a top-level section (e.g. "networks" or
// "volumes") and returns the names of entries that have external: true.
func externalResourceNames(doc *yaml.Node, section string) []string {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	top := doc.Content[0]
	sectionNode := mapValue(top, section)
	if sectionNode == nil || sectionNode.Kind != yaml.MappingNode {
		return nil
	}
	var result []string
	for i := 0; i < len(sectionNode.Content); i += 2 {
		name := sectionNode.Content[i].Value
		cfg := sectionNode.Content[i+1]
		if cfg.Kind != yaml.MappingNode {
			continue
		}
		ext := mapValue(cfg, "external")
		if ext != nil && ext.Value == "true" {
			result = append(result, name)
		}
	}
	return result
}

// BindMountSources returns the unique set of absolute host paths
// referenced as bind-mount sources across all services. Relative paths
// (repo-internal) and named-volume references are excluded.
func (p *Parsed) BindMountSources() []string {
	if p.doc.Kind != yaml.DocumentNode || len(p.doc.Content) == 0 {
		return nil
	}
	top := p.doc.Content[0]
	servicesNode := mapValue(top, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return nil
	}

	seen := map[string]bool{}
	var result []string

	for i := 0; i < len(servicesNode.Content); i += 2 {
		svcNode := servicesNode.Content[i+1]
		if svcNode.Kind != yaml.MappingNode {
			continue
		}
		vols := mapValue(svcNode, "volumes")
		if vols == nil {
			continue
		}
		switch vols.Kind {
		case yaml.SequenceNode:
			for _, item := range vols.Content {
				if src := bindMountSource(item); src != "" && !seen[src] {
					seen[src] = true
					result = append(result, src)
				}
			}
		}
	}
	return result
}

// bindMountSource extracts the absolute host path from a volume entry node,
// or returns "" if it is not an absolute-path bind mount.
func bindMountSource(n *yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		// Short form: "/abs/path:/container" or "named:/container"
		parts := strings.SplitN(n.Value, ":", 2)
		if len(parts) >= 1 && strings.HasPrefix(parts[0], "/") {
			return parts[0]
		}
	case yaml.MappingNode:
		// Long form: type: bind, source: /abs/path
		typeNode := mapValue(n, "type")
		if typeNode == nil || typeNode.Value != "bind" {
			return ""
		}
		src := mapValue(n, "source")
		if src != nil && strings.HasPrefix(src.Value, "/") {
			return src.Value
		}
	}
	return ""
}

// NetworkInspect returns true if the named docker network already exists.
func (c *Client) NetworkInspect(ctx context.Context, name string) (bool, error) {
	_, err := c.r.networkInspect(ctx, name)
	if err != nil {
		if isDockerNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// NetworkCreate creates a docker network with the default bridge driver.
func (c *Client) NetworkCreate(ctx context.Context, name string) error {
	return c.r.networkCreate(ctx, name)
}

// VolumeInspect returns true if the named docker volume already exists.
func (c *Client) VolumeInspect(ctx context.Context, name string) (bool, error) {
	_, err := c.r.volumeInspect(ctx, name)
	if err != nil {
		if isDockerNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// VolumeCreate creates a docker volume with the default local driver.
func (c *Client) VolumeCreate(ctx context.Context, name string) error {
	return c.r.volumeCreate(ctx, name)
}

// isDockerNotFound matches "No such network", "No such volume", etc.
func isDockerNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "No such network") ||
		strings.Contains(s, "no such network") ||
		strings.Contains(s, "No such volume") ||
		strings.Contains(s, "no such volume")
}
