// Package kubernetes holds typed builders for the Kubernetes manifests this
// workflow engine generates. Each type's ToMap produces the plain
// map[string]any tree that gets marshaled to YAML - see internal/manifest.
package kubernetes

// DataRef points an ExternalSecret field at a specific 1Password item/property.
type DataRef struct {
	SecretKey string
	Key       string
	Property  string
}

// ExternalSecret builds an external-secrets.io ExternalSecret manifest.
type ExternalSecret struct {
	Name         string
	APIVersion   string
	StoreName    string
	TemplateType string
	TemplateData map[string]any
	DataRefs     []DataRef
}

// ToMap renders the manifest as a generic document tree ready for YAML encoding.
func (s ExternalSecret) ToMap() map[string]any {
	data := make([]any, len(s.DataRefs))
	for i, ref := range s.DataRefs {
		data[i] = map[string]any{
			"secretKey": ref.SecretKey,
			// The 1Password ClusterSecretStore backend expects "item/field" as
			// a single key rather than separate key/property fields.
			"remoteRef": map[string]any{"key": ref.Key + "/" + ref.Property},
		}
	}

	return map[string]any{
		"apiVersion": s.APIVersion,
		"kind":       "ExternalSecret",
		"metadata":   map[string]any{"name": s.Name},
		"spec": map[string]any{
			"refreshInterval": "1h",
			"secretStoreRef": map[string]any{
				"name": s.StoreName,
				"kind": "ClusterSecretStore",
			},
			"target": map[string]any{
				"name":           s.Name,
				"creationPolicy": "Owner",
				"template": map[string]any{
					"type": s.TemplateType,
					"data": s.TemplateData,
				},
			},
			"data": data,
		},
	}
}
