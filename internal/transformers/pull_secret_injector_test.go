package transformers_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/manifest"
	"github.com/abradner/workflow/internal/transformers"
)

func newPullSecretInjector() transformers.PullSecretInjector {
	return transformers.PullSecretInjector{
		RegistryHostname:          "mock.registry.test",
		Registry1PItemID:          "mock_item_id",
		ExternalSecretsAPIVersion: "external-secrets.io/v1",
		ProjectName:               "wtf",
	}
}

func TestPullSecretInjector_GeneratesRegistryPullSecret(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev3"})

	out := newPullSecretInjector().Call(ws)

	doc := out.Manifests["base/registry-pull-secret.yaml"].(map[string]any)
	assert.Equal(t, "external-secrets.io/v1", doc["apiVersion"])
	assert.Equal(t, "ExternalSecret", doc["kind"])
	assert.Equal(t, "test-app-registry", manifest.DigString(doc, "metadata", "name"))

	spec := doc["spec"].(map[string]any)
	assert.Equal(t, "24h", spec["refreshInterval"])

	target := spec["target"].(map[string]any)
	assert.Equal(t, "test-app-registry", target["name"])
	template := target["template"].(map[string]any)
	assert.Equal(t, "kubernetes.io/dockerconfigjson", template["type"])

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(template["data"].(map[string]any)[".dockerconfigjson"].(string)), &parsed))
	auths := parsed["auths"].(map[string]any)
	assert.Contains(t, auths, "mock.registry.test")

	data := spec["data"].([]any)
	require.Len(t, data, 2)
	assert.Equal(t, "mock_item_id/username", manifest.DigString(data[0].(map[string]any), "remoteRef", "key"))
	assert.Equal(t, "mock_item_id/password", manifest.DigString(data[1].(map[string]any), "remoteRef", "key"))

	storeRef := spec["secretStoreRef"].(map[string]any)
	assert.Equal(t, "onepassword-backend", storeRef["name"])
	assert.Equal(t, "ClusterSecretStore", storeRef["kind"])
}

func TestPullSecretInjector_AddsImagePullSecretsToServiceAccount(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev3"})
	ws.Manifests["base/serviceaccount.yaml"] = map[string]any{
		"kind":     "ServiceAccount",
		"metadata": map[string]any{"name": "test-app"},
	}

	out := newPullSecretInjector().Call(ws)

	doc := out.Manifests["base/serviceaccount.yaml"].(map[string]any)
	pullSecrets := doc["imagePullSecrets"].([]any)
	assert.Equal(t, []any{map[string]any{"name": "test-app-registry"}}, pullSecrets)
}

func TestPullSecretInjector_AddsResourceToBaseKustomizationOnly(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev3"})
	ws.Manifests["base/kustomization.yaml"] = map[string]any{
		"resources": []any{"deployment.yaml"},
	}
	ws.Manifests["overlay/dev3/kustomization.yaml"] = map[string]any{
		"resources": []any{"../../base"},
	}

	out := newPullSecretInjector().Call(ws)

	base := out.Manifests["base/kustomization.yaml"].(map[string]any)
	assert.Contains(t, base["resources"], "registry-pull-secret.yaml")

	overlay := out.Manifests["overlay/dev3/kustomization.yaml"].(map[string]any)
	assert.NotContains(t, overlay["resources"], "registry-pull-secret.yaml")
}

func TestPullSecretInjector_RewritesOverlayImagePatches(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev3"})
	ws.Manifests["overlay/dev3/deployment.yaml"] = []any{
		map[string]any{"op": "replace", "path": "/spec/template/spec/containers/0/image", "value": "someregistry.io/test-app:v1"},
	}

	out := newPullSecretInjector().Call(ws)

	patches := out.Manifests["overlay/dev3/deployment.yaml"].([]any)
	patch := patches[0].(map[string]any)
	assert.Equal(t, "mock.registry.test/wtf/test-app:v1", patch["value"])
}
