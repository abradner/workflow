package transformers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/manifest"
	"github.com/abradner/workflow/internal/transformers"
)

func newModernizer() transformers.LegacyModernizer {
	return transformers.LegacyModernizer{
		ExternalSecretsAPIVersion: "external-secrets.io/v1",
		ProjectName:               "wtf",
		TLD:                       "f-ck.xyz",
	}
}

func TestLegacyModernizer_StripsTopologySpreadConstraints(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev3"})
	ws.Manifests["base/deployment.yaml"] = map[string]any{
		"kind": "Deployment",
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers":                []any{map[string]any{"name": "app"}},
					"topologySpreadConstraints": []any{map[string]any{"maxSkew": 1}},
				},
			},
		},
	}

	out := newModernizer().Call(ws)

	templateSpec := manifest.DigMap(out.Manifests["base/deployment.yaml"].(map[string]any), "spec", "template", "spec")
	assert.NotContains(t, templateSpec, "topologySpreadConstraints")
	assert.NotEmpty(t, templateSpec["containers"])
}

func TestLegacyModernizer_UpgradesBaseExternalSecret(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev3"})
	ws.Manifests["base/secrets.yaml"] = map[string]any{
		"kind":       "ExternalSecret",
		"apiVersion": "external-secrets.io/v1beta1",
		"metadata":   map[string]any{"name": "test-secret"},
		"spec":       map[string]any{"data": []any{}},
	}

	out := newModernizer().Call(ws)

	doc := out.Manifests["base/secrets.yaml"].(map[string]any)
	assert.Equal(t, "external-secrets.io/v1", doc["apiVersion"])
	spec := doc["spec"].(map[string]any)
	storeRef := spec["secretStoreRef"].(map[string]any)
	assert.Equal(t, "onepassword-backend", storeRef["name"])
	assert.Equal(t, "ClusterSecretStore", storeRef["kind"])
	assert.Equal(t, "1h", spec["refreshInterval"])
}

func TestLegacyModernizer_ConvertsBaseIngressToHTTPRoute(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev3"})
	ws.Manifests["base/ingress.yaml"] = map[string]any{
		"kind": "Ingress",
		"spec": map[string]any{
			"rules": []any{
				map[string]any{
					"host": "app.example.com",
					"http": map[string]any{
						"paths": []any{
							map[string]any{
								"backend": map[string]any{
									"service": map[string]any{
										"name": "app-svc",
										"port": map[string]any{"number": 8080},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	out := newModernizer().Call(ws)

	doc := out.Manifests["base/ingress.yaml"].(map[string]any)
	assert.Equal(t, "gateway.networking.k8s.io/v1", doc["apiVersion"])
	assert.Equal(t, "HTTPRoute", doc["kind"])

	spec := doc["spec"].(map[string]any)
	assert.Equal(t, []any{"app.example.com"}, spec["hostnames"])

	rules := spec["rules"].([]any)
	backendRefs := rules[0].(map[string]any)["backendRefs"].([]any)
	backendRef := backendRefs[0].(map[string]any)
	assert.Equal(t, "app-svc", backendRef["name"])
	assert.Equal(t, 8080, backendRef["port"])
}

func TestLegacyModernizer_KustomizationPatches(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev3"})
	ws.Manifests["base/secrets.yaml"] = map[string]any{
		"metadata": map[string]any{"name": "test-secret"},
	}
	ws.Manifests["overlay/dev3/kustomization.yaml"] = map[string]any{
		"kind": "Kustomization",
		"patches": []any{
			map[string]any{"path": "secrets.yaml"}, // legacy explicit secrets patch - must be dropped
			map[string]any{
				"target": map[string]any{"kind": "Ingress"},
			},
		},
	}

	out := newModernizer().Call(ws)

	doc := out.Manifests["overlay/dev3/kustomization.yaml"].(map[string]any)
	patches := doc["patches"].([]any)

	// The old bare {path: secrets.yaml} patch (no target) should be gone -
	// the only surviving "secrets.yaml" patch is the freshly injected one
	// that targets ExternalSecret (checked below).
	for _, p := range patches {
		patch := p.(map[string]any)
		if patch["path"] == "secrets.yaml" {
			assert.Contains(t, patch, "target", "the only secrets.yaml patch left should be the freshly injected one")
		}
	}

	var ingressPatch, secretPatch map[string]any
	for _, p := range patches {
		patch := p.(map[string]any)
		target, ok := patch["target"].(map[string]any)
		if !ok {
			continue
		}
		switch target["kind"] {
		case "HTTPRoute":
			ingressPatch = patch
		case "ExternalSecret":
			secretPatch = patch
		}
	}

	require.NotNil(t, ingressPatch, "Ingress-targeted patch should be upgraded to HTTPRoute")
	ingressTarget := ingressPatch["target"].(map[string]any)
	assert.Equal(t, "gateway.networking.k8s.io", ingressTarget["group"])
	assert.Equal(t, "v1", ingressTarget["version"])

	require.NotNil(t, secretPatch, "a fresh ExternalSecret-targeted secrets.yaml patch should be injected")
	secretTarget := secretPatch["target"].(map[string]any)
	assert.Equal(t, "external-secrets.io", secretTarget["group"])
	assert.Equal(t, "v1", secretTarget["version"])
	assert.Equal(t, "test-secret", secretTarget["name"])
}

func TestLegacyModernizer_OverlayIngressPatch(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev3"})
	ws.Manifests["overlay/dev3/ingress.yaml"] = []any{
		map[string]any{"op": "replace", "path": "/spec/rules/0/host", "value": "old-host"},
	}

	out := newModernizer().Call(ws)

	patches := out.Manifests["overlay/dev3/ingress.yaml"].([]any)
	patch := patches[0].(map[string]any)
	assert.Equal(t, "/spec/hostnames/0", patch["path"])
	assert.Equal(t, "test-app.wtf.dev3.f-ck.xyz", patch["value"])
}

func TestLegacyModernizer_OverlaySecretsPatch(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev3"})
	ws.Manifests["base/secrets.yaml"] = map[string]any{
		"metadata": map[string]any{"name": "test-secret"},
		"spec": map[string]any{
			"data": []any{
				map[string]any{"remoteRef": map[string]any{"key": "placeholder", "property": "secret_val"}},
			},
		},
	}
	ws.Manifests["overlay/dev3/secrets.yaml"] = []any{
		map[string]any{"op": "replace", "path": "/spec/data/0/remoteRef/key", "value": "dev3/wtf/config"},
	}

	out := newModernizer().Call(ws)

	patches := out.Manifests["overlay/dev3/secrets.yaml"].([]any)
	require.Len(t, patches, 2)

	replace := patches[0].(map[string]any)
	assert.Equal(t, "replace", replace["op"])
	assert.Equal(t, "/spec/data/0/remoteRef/key", replace["path"])
	assert.Equal(t, "k8s-wtf-dev3/wtf-config/secret_val", replace["value"])

	remove := patches[1].(map[string]any)
	assert.Equal(t, "remove", remove["op"])
	assert.Equal(t, "/spec/data/0/remoteRef/property", remove["path"])
}
