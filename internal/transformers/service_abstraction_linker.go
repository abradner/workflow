package transformers

import (
	"regexp"
	"strings"

	"github.com/abradner/workflow/internal/manifest"
	"github.com/abradner/workflow/internal/services/endpointmapper"
)

// ServiceAbstractionLinker rewrites known external hostnames referenced in
// Kustomize configMapGenerator literals into cluster-local DNS names, and
// generates the ExternalName Service manifests that back them.
type ServiceAbstractionLinker struct {
	ProjectName string
	TLD         string
}

// serviceRef is a discovered external-service reference: the app-prefixed
// cluster-local resource name, and the external hostname it replaces.
type serviceRef struct {
	Resource string
	ExtDNS   string
}

// Call scans every overlay Kustomization for configMapGenerator literals
// that reference known external endpoints, rewrites them to cluster-local
// DNS, and emits an external-services.yaml with the backing ExternalName
// Services.
func (t ServiceAbstractionLinker) Call(ws *manifest.Workspace) *manifest.Workspace {
	// Snapshot the paths before mutating: generateExternalServices adds a
	// new "overlay/<env>/external-services.yaml" key to ws.Manifests, and
	// adding to a Go map mid-range is undefined as to whether the new key
	// gets visited (the same reason Ruby's original does `.to_a` here).
	for _, path := range ws.SortedPaths() {
		doc, ok := ws.Manifests[path].(map[string]any)
		if !ok || doc["kind"] != "Kustomization" || !strings.HasPrefix(path, "overlay/") {
			continue
		}

		env := manifest.ExtractEnv(path)
		generators, _ := doc["configMapGenerator"].([]any)
		discovered := processConfigMapGenerator(generators, env, ws.AppName, t.ProjectName, t.TLD)

		if len(discovered) == 0 {
			continue
		}

		t.generateExternalServices(discovered, env, ws)

		resources, _ := doc["resources"].([]any)
		if !containsString(resources, "external-services.yaml") {
			resources = append(resources, "external-services.yaml")
		}
		doc["resources"] = resources
	}

	return ws
}

func processConfigMapGenerator(generators []any, env, appName, projectName, tld string) []serviceRef {
	var discovered []serviceRef

	for _, cmAny := range generators {
		cm, ok := cmAny.(map[string]any)
		if !ok {
			continue
		}
		literals, ok := cm["literals"].([]any)
		if !ok {
			continue
		}

		for i, litAny := range literals {
			lit, _ := litAny.(string)
			services, mappedLit := mapLiteral(lit, env, appName, projectName, tld)
			discovered = append(discovered, services...)
			literals[i] = mappedLit
		}
		cm["literals"] = literals
	}

	return uniqServiceRefs(discovered)
}

func mapLiteral(lit, env, appName, projectName, tld string) ([]serviceRef, string) {
	var services []serviceRef

	if strings.Contains(lit, "=") {
		parts := strings.SplitN(lit, "=", 2)
		key, value := parts[0], parts[1]

		var suffixServices []serviceRef
		value, suffixServices = mapSuffixPatterns(value, env, appName, projectName, tld)
		services = append(services, suffixServices...)

		lit = key + "=" + value
	}

	var knownServices []serviceRef
	lit, knownServices = mapKnownServices(lit, env, appName, projectName, tld)
	services = append(services, knownServices...)

	return services, lit
}

// mapSuffixPatterns matches suffix-identified hostnames (RDS, Confluent) in
// value, rewrites each to its cluster-local DNS name, and reports every
// service discovered along the way.
func mapSuffixPatterns(value, env, appName, projectName, tld string) (string, []serviceRef) {
	var services []serviceRef

	for _, m := range endpointmapper.SuffixMappings {
		bareSuffix := strings.TrimPrefix(m.Suffix, ".")
		if !strings.Contains(value, bareSuffix) {
			continue
		}

		pattern := regexp.MustCompile(`[a-zA-Z0-9._-]+` + regexp.QuoteMeta(m.Suffix))
		for _, matchedHost := range pattern.FindAllString(value, -1) {
			prefixedResource := appName + "-" + m.Resource
			clusterDNS := prefixedResource + "." + projectName + "-" + env + ".svc.cluster.local"
			value = strings.ReplaceAll(value, matchedHost, clusterDNS)

			extDNS := m.Resource + "." + projectName + "." + env + "." + tld
			services = append(services, serviceRef{Resource: prefixedResource, ExtDNS: extDNS})
		}
	}

	return value, services
}

// mapKnownServices matches the fixed set of known service hostnames
// (pg/kafka/redis, rendered as "<resource>.<project>.<env>.<tld>") anywhere
// in lit and rewrites them to cluster-local DNS.
func mapKnownServices(lit, env, appName, projectName, tld string) (string, []serviceRef) {
	var services []serviceRef

	for _, resource := range endpointmapper.KnownServices {
		extDNS := resource + "." + projectName + "." + env + "." + tld
		prefixedResource := appName + "-" + resource
		clusterDNS := prefixedResource + "." + projectName + "-" + env + ".svc.cluster.local"

		if strings.Contains(lit, extDNS) {
			lit = strings.ReplaceAll(lit, extDNS, clusterDNS)
			services = append(services, serviceRef{Resource: prefixedResource, ExtDNS: extDNS})
		}
	}

	return lit, services
}

func (t ServiceAbstractionLinker) generateExternalServices(services []serviceRef, env string, ws *manifest.Workspace) {
	docs := make([]any, len(services))
	for i, srv := range services {
		docs[i] = map[string]any{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata":   map[string]any{"name": srv.Resource, "namespace": t.ProjectName + "-" + env},
			"spec":       map[string]any{"type": "ExternalName", "externalName": srv.ExtDNS},
		}
	}
	ws.Manifests["overlay/"+env+"/external-services.yaml"] = docs
}

func uniqServiceRefs(refs []serviceRef) []serviceRef {
	seen := make(map[serviceRef]bool, len(refs))
	out := make([]serviceRef, 0, len(refs))
	for _, r := range refs {
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

func containsString(s []any, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}
