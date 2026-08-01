// Package onepassword builds the "one Secure Note item per environment"
// vault payload the workflow engine provisions from extracted AWS secrets.
package onepassword

import (
	"context"
	"fmt"
	"strings"

	"github.com/abradner/workflow/internal/domain"
	"github.com/abradner/workflow/internal/transformers"
)

// Client is the subset of the 1Password CLI wrapper this service needs.
type Client interface {
	CreateItem(ctx context.Context, item map[string]any, vault string) (string, error)
}

// Service builds and creates 1Password vault items from extracted secrets.
type Service struct {
	client      Client
	projectName string
	vaultName   string
}

// New builds a Service for the given project, writing into vaultName.
func New(projectName, vaultName string, client Client) *Service {
	return &Service{client: client, projectName: projectName, vaultName: vaultName}
}

// IngestVaultItem builds a single Secure Note titled "k8s-<project>-<env>"
// from extractedSecrets - one section per source secret, with either its
// JSON object's fields spread out individually, or a single "password"
// field for opaque string/binary secrets - and creates it in 1Password.
func (s *Service) IngestVaultItem(ctx context.Context, env string, extractedSecrets []domain.ExtractedSecret) (string, error) {
	itemTitle := fmt.Sprintf("k8s-%s-%s", s.projectName, env)

	sections := make([]map[string]any, 0, len(extractedSecrets))
	fields := make([]map[string]any, 0, len(extractedSecrets))

	for _, secret := range extractedSecrets {
		sectionID := sanitizeSectionID(secret.Name)
		sections = append(sections, map[string]any{"id": sectionID, "label": sectionID})

		switch {
		case secret.String != nil:
			if keys, values, ok := transformers.ParseFlatJSONObject(*secret.String); ok {
				for _, k := range keys {
					fields = append(fields, concealedField(sectionID, k, transformers.Stringify(values[k])))
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

	return s.client.CreateItem(ctx, opTemplate, s.vaultName)
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
