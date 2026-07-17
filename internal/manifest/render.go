package manifest

import (
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// IsYAMLPath reports whether path looks like a YAML file by extension.
func IsYAMLPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

// RenderYAML serializes doc to its final YAML text. It's pure - no I/O -
// which is what lets it run directly in Temporal workflow code once a
// workspace's manifests are down to plain strings (see
// internal/activities.BuildAppFiles for why the doc trees themselves stay
// inside the activity instead).
//
// Kubernetes/Kustomize multi-document streams need "---" separators between
// documents; a JSON-patch array or a plain config hash doesn't. A stream is
// detected the same way the original tool did: it's a []any where at least
// one element is a map carrying a "kind" key.
func RenderYAML(doc any) (string, error) {
	if items, ok := doc.([]any); ok && isDocumentStream(items) {
		parts := make([]string, len(items))
		for i, item := range items {
			b, err := yaml.Marshal(item)
			if err != nil {
				return "", err
			}
			parts[i] = string(b)
		}
		return strings.Join(parts, "---\n"), nil
	}

	b, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func isDocumentStream(items []any) bool {
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			if _, hasKind := m["kind"]; hasKind {
				return true
			}
		}
	}
	return false
}
