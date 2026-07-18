// Package transformers is the pure, side-effect-free part of the sync
// pipeline: every function here takes a *manifest.Workspace and returns it
// mutated, with no I/O and no dependency on anything outside its arguments.
// That purity is exactly what lets a Temporal workflow call these directly
// instead of wrapping them in activities - see internal/workflows/syncworkloads.go.
package transformers

import (
	"slices"
	"strings"

	"github.com/abradner/workflow/internal/manifest"
)

// EnvironmentGenerator clones the source environment's overlay files into
// every other target environment, rewriting embedded env-name references as
// it goes. It must run first in the pipeline - everything downstream expects
// the full set of target-env overlays to already exist.
func EnvironmentGenerator(ws *manifest.Workspace) *manifest.Workspace {
	sourceEnv := ws.SourceEnv
	overlayFiles := ws.SourceOverlayFiles()

	for _, env := range ws.TargetEnvs {
		if env == sourceEnv {
			continue
		}

		for virtualPath, content := range overlayFiles {
			newPath := strings.Replace(virtualPath, "overlay/"+sourceEnv, "overlay/"+env, 1)
			ws.Manifests[newPath] = deepReplace(content, sourceEnv, env)
		}
	}

	// Drop the source environment's own files unless it was explicitly
	// requested as one of the deployment targets.
	if !slices.Contains(ws.TargetEnvs, sourceEnv) {
		prefix := "overlay/" + sourceEnv + "/"
		for path := range ws.Manifests {
			if strings.HasPrefix(path, prefix) {
				delete(ws.Manifests, path)
			}
		}
	}

	return ws
}

// deepReplace walks a parsed-YAML value tree, returning a deep copy with
// every occurrence of source replaced by target in every string leaf.
func deepReplace(node any, source, target string) any {
	switch v := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = deepReplace(val, source, target)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = deepReplace(val, source, target)
		}
		return out
	case string:
		return strings.ReplaceAll(v, source, target)
	default:
		return node
	}
}
