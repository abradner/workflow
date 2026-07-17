package transformers

import (
	"encoding/json"
	"strings"

	"github.com/abradner/workflow/internal/domain/kubernetes"
	"github.com/abradner/workflow/internal/manifest"
)

// PullSecretInjector synthesizes a registry pull secret (backed by a
// 1Password item) for the app, wires it into ServiceAccounts and the base
// Kustomization, and rewrites overlay deployment image patches to pull
// through the configured registry.
type PullSecretInjector struct {
	RegistryHostname          string
	Registry1PItemID          string
	ExternalSecretsAPIVersion string
	ProjectName               string
}

// Call injects the registry pull secret across the workspace.
func (t PullSecretInjector) Call(ws *manifest.Workspace) *manifest.Workspace {
	dockerConfigBytes, _ := json.Marshal(map[string]any{
		"auths": map[string]any{
			t.RegistryHostname: map[string]any{
				"username": "{{ .username }}",
				"password": "{{ .password }}",
			},
		},
	})

	uniqueSecretName := ws.AppName + "-registry"

	secret := kubernetes.ExternalSecret{
		Name:         uniqueSecretName,
		APIVersion:   t.ExternalSecretsAPIVersion,
		StoreName:    "onepassword-backend",
		TemplateType: "kubernetes.io/dockerconfigjson",
		TemplateData: map[string]any{".dockerconfigjson": string(dockerConfigBytes)},
		DataRefs: []kubernetes.DataRef{
			{SecretKey: "username", Key: t.Registry1PItemID, Property: "username"},
			{SecretKey: "password", Key: t.Registry1PItemID, Property: "password"},
		},
	}

	secretDoc := secret.ToMap()
	spec, _ := secretDoc["spec"].(map[string]any)
	spec["refreshInterval"] = "24h"
	secretDoc["spec"] = spec

	ws.Manifests["base/registry-pull-secret.yaml"] = secretDoc

	for _, path := range ws.SortedPaths() {
		docs := ws.Manifests[path]

		if docsMap, ok := docs.(map[string]any); ok &&
			(strings.Contains(path, "kustomization.yaml") || strings.Contains(path, "kustomization.yml")) {
			if strings.HasPrefix(path, "base/") {
				resources, _ := docsMap["resources"].([]any)
				if !containsString(resources, "registry-pull-secret.yaml") {
					resources = append(resources, "registry-pull-secret.yaml")
				}
				docsMap["resources"] = resources
			}
			continue
		}

		mutated := manifest.MutateYAML(docs, func(d any) any {
			doc, ok := d.(map[string]any)
			if !ok {
				return d
			}
			if doc["kind"] == "ServiceAccount" {
				pullSecrets, _ := doc["imagePullSecrets"].([]any)
				if !containsPullSecretName(pullSecrets, uniqueSecretName) {
					pullSecrets = append(pullSecrets, map[string]any{"name": uniqueSecretName})
				}
				doc["imagePullSecrets"] = pullSecrets
			}
			return doc
		})

		if strings.Contains(path, "deployment.yaml") && strings.HasPrefix(path, "overlay/") {
			if patches, ok := mutated.([]any); ok {
				rewriteImagePatches(patches, t.RegistryHostname, t.ProjectName)
			}
		}

		ws.Manifests[path] = mutated
	}

	return ws
}

func rewriteImagePatches(patches []any, registryHostname, projectName string) {
	for _, patchAny := range patches {
		patch, ok := patchAny.(map[string]any)
		if !ok {
			continue
		}
		patchPath, _ := patch["path"].(string)
		if patch["op"] != "replace" || !strings.Contains(patchPath, "image") {
			continue
		}

		value, _ := patch["value"].(string)
		parts := strings.Split(value, "/")
		imageRepoTag := parts[len(parts)-1]
		patch["value"] = registryHostname + "/" + projectName + "/" + imageRepoTag
	}
}

func containsPullSecretName(secrets []any, name string) bool {
	for _, sAny := range secrets {
		if s, ok := sAny.(map[string]any); ok && s["name"] == name {
			return true
		}
	}
	return false
}
