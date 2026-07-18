// Package templaterendering implements the {{ dotted.key }} placeholder
// substitution used to hydrate Talos bootstrap templates from a flattened
// secrets document. It's plain string/regex logic deliberately kept separate
// from Go's text/template, whose "{{ .Field }}" dot-path semantics don't
// match this tool's flat "{{ dotted.key }}" lookup.
package templaterendering

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var placeholderRegex = regexp.MustCompile(`\{\{\s*([\w.]+)\s*\}\}`)

// FlattenHash recursively flattens a nested map into dot-separated keys,
// e.g. {"cluster": {"id": "abc"}} -> {"cluster.id": "abc"}.
func FlattenHash(m map[string]any) map[string]any {
	return flattenHash(m, "")
}

func flattenHash(m map[string]any, parentKey string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		newKey := k
		if parentKey != "" {
			newKey = parentKey + "." + k
		}
		if nested, ok := v.(map[string]any); ok {
			for nk, nv := range flattenHash(nested, newKey) {
				out[nk] = nv
			}
		} else {
			out[newKey] = v
		}
	}
	return out
}

// ExtractPlaceholders returns the sorted, deduplicated set of "{{ key }}"
// placeholder keys found in templateContent.
func ExtractPlaceholders(templateContent string) []string {
	matches := placeholderRegex.FindAllStringSubmatch(templateContent, -1)

	seen := make(map[string]bool, len(matches))
	keys := make([]string, 0, len(matches))
	for _, m := range matches {
		key := m[1]
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)
	return keys
}

// MissingKeys returns every placeholder in templateContent that has no
// corresponding entry in flatSecrets.
func MissingKeys(templateContent string, flatSecrets map[string]any) []string {
	var missing []string
	for _, key := range ExtractPlaceholders(templateContent) {
		if _, ok := flatSecrets[key]; !ok {
			missing = append(missing, key)
		}
	}
	return missing
}

// Render replaces every "{{ key }}" placeholder in templateContent with its
// value from flatSecrets. Returns an error naming every unresolved
// placeholder if any are missing - no partial output.
func Render(templateContent string, flatSecrets map[string]any) (string, error) {
	missing := MissingKeys(templateContent, flatSecrets)
	if len(missing) > 0 {
		return "", fmt.Errorf("missing secret keys: %s", strings.Join(missing, ", "))
	}

	rendered := placeholderRegex.ReplaceAllStringFunc(templateContent, func(match string) string {
		key := placeholderRegex.FindStringSubmatch(match)[1]
		return stringify(flatSecrets[key])
	})
	return rendered, nil
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
