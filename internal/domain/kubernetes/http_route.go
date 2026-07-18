package kubernetes

// HTTPRoute builds a Gateway API HTTPRoute manifest.
type HTTPRoute struct {
	Name        string
	Namespace   string
	Hostnames   []string
	BackendRefs []map[string]any
}

// ToMap renders the manifest as a generic document tree ready for YAML encoding.
func (r HTTPRoute) ToMap() map[string]any {
	hostnames := r.Hostnames
	if len(hostnames) == 0 {
		hostnames = []string{"placeholder.local"}
	}

	backendRefs := make([]any, len(r.BackendRefs))
	for i, ref := range r.BackendRefs {
		backendRefs[i] = ref
	}

	hostnamesAny := make([]any, len(hostnames))
	for i, h := range hostnames {
		hostnamesAny[i] = h
	}

	return map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata":   map[string]any{"name": r.Name, "namespace": r.Namespace},
		"spec": map[string]any{
			"parentRefs": []any{map[string]any{"name": "homelab-gateway", "namespace": "default"}},
			"hostnames":  hostnamesAny,
			"rules":      []any{map[string]any{"backendRefs": backendRefs}},
		},
	}
}

// FromIngress converts a legacy Ingress document into an HTTPRoute manifest map.
func FromIngress(doc map[string]any) map[string]any {
	metadata, _ := doc["metadata"].(map[string]any)
	name, _ := metadata["name"].(string)
	namespace, _ := metadata["namespace"].(string)
	if namespace == "" {
		namespace = "default"
	}

	spec, _ := doc["spec"].(map[string]any)
	rulesRaw, _ := spec["rules"].([]any)

	var hostnames []string
	var backendRefs []map[string]any

	for _, ruleAny := range rulesRaw {
		rule, ok := ruleAny.(map[string]any)
		if !ok {
			continue
		}

		if host, ok := rule["host"].(string); ok && host != "" {
			hostnames = append(hostnames, host)
		}

		http, _ := rule["http"].(map[string]any)
		paths, _ := http["paths"].([]any)

		for _, pathAny := range paths {
			path, ok := pathAny.(map[string]any)
			if !ok {
				continue
			}

			backend, _ := path["backend"].(map[string]any)
			svc, ok := backend["service"].(map[string]any)
			if !ok || svc == nil {
				continue
			}

			port, _ := svc["port"].(map[string]any)
			backendRefs = append(backendRefs, map[string]any{
				"name": svc["name"],
				"port": port["number"],
			})
		}
	}

	return HTTPRoute{Name: name, Namespace: namespace, Hostnames: hostnames, BackendRefs: backendRefs}.ToMap()
}
