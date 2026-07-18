package kubernetes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abradner/workflow/internal/domain/kubernetes"
	"github.com/abradner/workflow/internal/manifest"
)

func TestExternalSecret_ToMap(t *testing.T) {
	secret := kubernetes.ExternalSecret{
		Name:         "test-registry",
		APIVersion:   "external-secrets.io/v1",
		StoreName:    "onepassword-backend",
		TemplateType: "kubernetes.io/dockerconfigjson",
		TemplateData: map[string]any{".dockerconfigjson": "{}"},
		DataRefs: []kubernetes.DataRef{
			{SecretKey: "username", Key: "item123", Property: "username"},
			{SecretKey: "password", Key: "item123", Property: "password"},
		},
	}

	doc := secret.ToMap()

	assert.Equal(t, "external-secrets.io/v1", doc["apiVersion"])
	assert.Equal(t, "ExternalSecret", doc["kind"])
	assert.Equal(t, "test-registry", manifest.DigString(doc, "metadata", "name"))
	assert.Equal(t, "1h", manifest.DigString(doc, "spec", "refreshInterval"))

	storeRef := manifest.DigMap(doc, "spec", "secretStoreRef")
	assert.Equal(t, "onepassword-backend", storeRef["name"])
	assert.Equal(t, "ClusterSecretStore", storeRef["kind"])

	data := manifest.DigSlice(doc, "spec", "data")
	assert.Equal(t, "item123/username", manifest.DigString(data[0].(map[string]any), "remoteRef", "key"))
	assert.Equal(t, "item123/password", manifest.DigString(data[1].(map[string]any), "remoteRef", "key"))
}

func TestHTTPRoute_ToMap(t *testing.T) {
	t.Run("uses a placeholder hostname when none are given", func(t *testing.T) {
		route := kubernetes.HTTPRoute{Name: "app", Namespace: "default"}
		doc := route.ToMap()
		assert.Equal(t, []any{"placeholder.local"}, manifest.Dig(doc, "spec", "hostnames"))
	})

	t.Run("carries through given hostnames and backend refs", func(t *testing.T) {
		route := kubernetes.HTTPRoute{
			Name: "app", Namespace: "default",
			Hostnames:   []string{"app.example.com"},
			BackendRefs: []map[string]any{{"name": "app-svc", "port": 8080}},
		}
		doc := route.ToMap()
		assert.Equal(t, "gateway.networking.k8s.io/v1", doc["apiVersion"])
		assert.Equal(t, []any{"app.example.com"}, manifest.Dig(doc, "spec", "hostnames"))

		rules := manifest.DigSlice(doc, "spec", "rules")
		backendRefs := rules[0].(map[string]any)["backendRefs"].([]any)
		assert.Equal(t, "app-svc", backendRefs[0].(map[string]any)["name"])
	})
}

func TestFromIngress(t *testing.T) {
	ingress := map[string]any{
		"metadata": map[string]any{"name": "app", "namespace": "wtf-dev4"},
		"spec": map[string]any{
			"rules": []any{
				map[string]any{
					"host": "app.example.com",
					"http": map[string]any{
						"paths": []any{
							map[string]any{"backend": map[string]any{"service": map[string]any{
								"name": "app-svc",
								"port": map[string]any{"number": 8080},
							}}},
						},
					},
				},
			},
		},
	}

	doc := kubernetes.FromIngress(ingress)

	assert.Equal(t, "HTTPRoute", doc["kind"])
	assert.Equal(t, "app", manifest.DigString(doc, "metadata", "name"))
	assert.Equal(t, "wtf-dev4", manifest.DigString(doc, "metadata", "namespace"))
	assert.Equal(t, []any{"app.example.com"}, manifest.Dig(doc, "spec", "hostnames"))

	rules := manifest.DigSlice(doc, "spec", "rules")
	backendRefs := rules[0].(map[string]any)["backendRefs"].([]any)
	ref := backendRefs[0].(map[string]any)
	assert.Equal(t, "app-svc", ref["name"])
	assert.Equal(t, 8080, ref["port"])
}
