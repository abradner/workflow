// Package onepassword builds the "one Secure Note item per environment"
// vault payload the workflow engine provisions from extracted AWS secrets.
package onepassword

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/abradner/workflow/internal/domain"
)

// Client is the subset of the 1Password CLI wrapper this service needs.
type Client interface {
	CreateItem(ctx context.Context, item map[string]any) (string, error)
}

// Service builds and creates 1Password vault items from extracted secrets.
type Service struct {
	client      Client
	projectName string
}

// New builds a Service for the given project.
func New(projectName string, client Client) *Service {
	return &Service{client: client, projectName: projectName}
}

// IngestVaultItem builds a single Secure Note titled "k8s-<project>-<env>"
// from extractedSecrets - one section per source secret, with either its
// JSON object's fields spread out individually, or a single "password"
// field for opaque string/binary secrets - and creates it in 1Password.
func (s *Service) IngestVaultItem(ctx context.Context, env string, extractedSecrets []domain.ExtractedSecret) (string, error) {
	itemTitle := fmt.Sprintf("k8s-%s-%s", s.projectName, env)

	sections := make([]map[string]any, 0, len(extractedSecrets))
	var fields []map[string]any

	for _, secret := range extractedSecrets {
		sectionID := sanitizeSectionID(secret.Name)
		sections = append(sections, map[string]any{"id": sectionID, "label": sectionID})

		switch {
		case secret.String != nil:
			if keys, values, ok := parseFlatJSONObject(*secret.String); ok {
				for _, k := range keys {
					fields = append(fields, concealedField(sectionID, k, fmt.Sprint(values[k])))
				}
			} else {
				fields = append(fields, concealedField(sectionID, "password", *secret.String))
			}
		case secret.Binary != nil:
			fields = append(fields, concealedField(sectionID, "password", *secret.Binary))
		}
	}

	opTemplate := map[string]any{
		"title":    itemTitle,
		"category": "SECURE_NOTE",
		"sections": sections,
		"fields":   fields,
	}

	return s.client.CreateItem(ctx, opTemplate)
}

func concealedField(sectionID, label, value string) map[string]any {
	return map[string]any{
		"section": map[string]any{"id": sectionID},
		"label":   label,
		"value":   value,
		"type":    "CONCEALED",
	}
}

// sanitizeSectionID drops the leading environment segment from an AWS
// secret name and joins the rest with hyphens, e.g. "dev3/wtf/config" ->
// "wtf-config".
func sanitizeSectionID(awsName string) string {
	parts := strings.Split(awsName, "/")
	if len(parts) > 1 {
		parts = parts[1:]
	}
	return strings.Join(parts, "-")
}

// parseFlatJSONObject decodes a JSON object's top-level fields, preserving
// their original order (encoding/json's map decoding does not - Go map
// iteration order is randomized on purpose). Returns ok=false if s isn't a
// JSON object.
func parseFlatJSONObject(s string) (keys []string, values map[string]any, ok bool) {
	dec := json.NewDecoder(strings.NewReader(s))

	tok, err := dec.Token()
	if err != nil {
		return nil, nil, false
	}
	if delim, isDelim := tok.(json.Delim); !isDelim || delim != '{' {
		return nil, nil, false
	}

	values = map[string]any{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, nil, false
		}
		key, isString := keyTok.(string)
		if !isString {
			return nil, nil, false
		}

		var value any
		if err := dec.Decode(&value); err != nil {
			return nil, nil, false
		}

		keys = append(keys, key)
		values[key] = value
	}

	if _, err := dec.Token(); err != nil { // consume closing '}'
		return nil, nil, false
	}
	if dec.More() {
		return nil, nil, false // trailing content after the object
	}

	return keys, values, true
}
