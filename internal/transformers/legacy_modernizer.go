package transformers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/abradner/workflow/internal/manifest"
)

// LegacyModernizer migrates aging manifest shapes into their modern
// cloud-native equivalents: Ingress -> HTTPRoute, AWS Secrets Manager
// references -> 1Password ExternalSecret references, and a few other
// cleanups (stripping topologySpreadConstraints, upgrading ExternalSecret
// API versions).
type LegacyModernizer struct {
	ExternalSecretsAPIVersion string
	ProjectName               string
	TLD                       string
}

// Call applies every modernization rule across the workspace's manifests.
func (t LegacyModernizer) Call(ws *manifest.Workspace) *manifest.Workspace {
	for path, docs := range ws.Manifests {
		if m, ok := docs.(map[string]any); ok &&
			(strings.Contains(path, "kustomization.yaml") || strings.Contains(path, "kustomization.yml")) &&
			strings.HasPrefix(path, "overlay/") {
			t.modernizeKustomization(m, ws)
			continue
		}

		mutated := manifest.MutateYAML(docs, func(d any) any {
			doc, ok := d.(map[string]any)
			if !ok {
				return d
			}
			t.modernizeBaseExternalSecrets(doc, path)
			stripTopology(doc)
			modernizeBaseIngress(doc, path)
			t.modernizeOverlayIngressPatches(doc, path, ws)
			return doc
		})

		if strings.Contains(path, "secrets.yaml") && strings.HasPrefix(path, "overlay/") {
			mutated = t.modernizeOverlaySecretsPatches(mutated, path, ws)
		}

		ws.Manifests[path] = mutated
	}

	return ws
}

func (t LegacyModernizer) modernizeKustomization(doc map[string]any, ws *manifest.Workspace) {
	patchesRaw, ok := doc["patches"].([]any)
	if !ok {
		return
	}

	// Drop the old explicit secrets.yaml patch entry entirely...
	filtered := make([]any, 0, len(patchesRaw))
	for _, pAny := range patchesRaw {
		if p, ok := pAny.(map[string]any); ok && p["path"] == "secrets.yaml" {
			continue
		}
		filtered = append(filtered, pAny)
	}

	// ...and upgrade any Ingress-targeted patch to target HTTPRoute instead.
	for _, pAny := range filtered {
		p, ok := pAny.(map[string]any)
		if !ok {
			continue
		}
		target, ok := p["target"].(map[string]any)
		if ok && target["kind"] == "Ingress" {
			target["group"] = "gateway.networking.k8s.io"
			target["version"] = "v1"
			target["kind"] = "HTTPRoute"
		}
	}

	baseSecret := baseSecretDoc(ws)
	if baseSecret != nil {
		secretName := manifest.DigString(baseSecret, "metadata", "name")
		esVersion := lastSegment(t.ExternalSecretsAPIVersion)

		filtered = append(filtered, map[string]any{
			"path": "secrets.yaml",
			"target": map[string]any{
				"group":   "external-secrets.io",
				"version": esVersion,
				"kind":    "ExternalSecret",
				"name":    secretName,
			},
		})
	}

	doc["patches"] = filtered
}

func (t LegacyModernizer) modernizeBaseExternalSecrets(doc map[string]any, path string) {
	if doc["kind"] != "ExternalSecret" || !strings.HasPrefix(path, "base/") {
		return
	}

	doc["apiVersion"] = t.ExternalSecretsAPIVersion
	spec, ok := doc["spec"].(map[string]any)
	if !ok {
		spec = map[string]any{}
	}
	spec["secretStoreRef"] = map[string]any{"name": "onepassword-backend", "kind": "ClusterSecretStore"}
	spec["refreshInterval"] = "1h"
	doc["spec"] = spec
}

func stripTopology(doc map[string]any) {
	if doc["kind"] != "Deployment" {
		return
	}
	if templateSpec := manifest.DigMap(doc, "spec", "template", "spec"); templateSpec != nil {
		delete(templateSpec, "topologySpreadConstraints")
	}
}

func modernizeBaseIngress(doc map[string]any, path string) {
	if doc["kind"] != "Ingress" || !strings.HasPrefix(path, "base/") {
		return
	}

	doc["apiVersion"] = "gateway.networking.k8s.io/v1"
	doc["kind"] = "HTTPRoute"

	rules := manifest.DigSlice(doc, "spec", "rules")
	if len(rules) == 0 {
		return
	}
	rule, ok := rules[0].(map[string]any)
	if !ok {
		return
	}

	hostnames := []any{}
	if hostValue, ok := rule["host"].(string); ok && hostValue != "" {
		hostnames = append(hostnames, hostValue)
	}

	var serviceName, servicePort any
	if paths := manifest.DigSlice(rule, "http", "paths"); len(paths) > 0 {
		if p0, ok := paths[0].(map[string]any); ok {
			if svc := manifest.DigMap(p0, "backend", "service"); svc != nil {
				serviceName = svc["name"]
				servicePort = manifest.Dig(svc, "port", "number")
			}
		}
	}

	doc["spec"] = map[string]any{
		"parentRefs": []any{map[string]any{"name": "homelab-gateway", "namespace": "default"}},
		"hostnames":  hostnames,
		"rules":      []any{map[string]any{"backendRefs": []any{compactMap(map[string]any{"name": serviceName, "port": servicePort})}}},
	}
}

func (t LegacyModernizer) modernizeOverlayIngressPatches(doc map[string]any, path string, ws *manifest.Workspace) {
	if !strings.Contains(path, "ingress.yaml") || !strings.HasPrefix(path, "overlay/") {
		return
	}

	env := manifest.ExtractEnv(path)
	fqdn := fmt.Sprintf("%s.%s.%s.%s", ws.AppName, t.ProjectName, env, t.TLD)

	docPath, _ := doc["path"].(string)
	if doc["op"] == "replace" && strings.Contains(docPath, "host") {
		doc["path"] = "/spec/hostnames/0"
		doc["value"] = fqdn
	}
}

func (t LegacyModernizer) modernizeOverlaySecretsPatches(docs any, path string, ws *manifest.Workspace) any {
	baseSecret := baseSecretDoc(ws)
	if baseSecret == nil {
		return docs
	}

	docsArray, ok := docs.([]any)
	if !ok {
		return docs
	}

	env := manifest.ExtractEnv(path)
	out := make([]any, 0, len(docsArray))

	for _, patchAny := range docsArray {
		patch, ok := patchAny.(map[string]any)
		patchPath, _ := patch["path"].(string)

		if !ok || patch["op"] != "replace" || !strings.Contains(patchPath, "remoteRef/key") {
			out = append(out, patchAny)
			continue
		}

		awsVal, _ := patch["value"].(string)
		parts := strings.Split(awsVal, "/")
		if len(parts) > 1 {
			parts = parts[1:] // drop the leading env segment, e.g. dev3/wtf/config -> wtf/config
		}
		sectionID := strings.Join(parts, "-")

		pathParts := strings.Split(patchPath, "/")
		index := 0
		if len(pathParts) > 3 {
			index, _ = strconv.Atoi(pathParts[3])
		}

		baseProperty := "password"
		if dataSlice := manifest.DigSlice(baseSecret, "spec", "data"); index >= 0 && index < len(dataSlice) {
			if item, ok := dataSlice[index].(map[string]any); ok {
				if prop := manifest.DigString(item, "remoteRef", "property"); prop != "" {
					baseProperty = prop
				}
			}
		}

		newKey := fmt.Sprintf("k8s-%s-%s/%s/%s", t.ProjectName, env, sectionID, baseProperty)
		out = append(out,
			map[string]any{"op": "replace", "path": patchPath, "value": newKey},
			map[string]any{"op": "remove", "path": fmt.Sprintf("/spec/data/%d/remoteRef/property", index)},
		)
	}

	return out
}

// baseSecretDoc returns base/secrets.yaml as a single document, unwrapping
// the first element if it was parsed as a one-item document stream.
func baseSecretDoc(ws *manifest.Workspace) map[string]any {
	switch v := ws.Manifests["base/secrets.yaml"].(type) {
	case []any:
		if len(v) > 0 {
			m, _ := v[0].(map[string]any)
			return m
		}
	case map[string]any:
		return v
	}
	return nil
}

func lastSegment(s string) string {
	parts := strings.Split(s, "/")
	return parts[len(parts)-1]
}

// compactMap returns a copy of m with nil-valued keys removed, mirroring
// Ruby's Hash#compact.
func compactMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if v != nil {
			out[k] = v
		}
	}
	return out
}
