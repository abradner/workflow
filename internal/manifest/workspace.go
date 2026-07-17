// Package manifest models the in-memory tree of Kubernetes/Kustomize files
// for a single application, and the small set of pure helpers the transformer
// pipeline uses to read and mutate it. Nothing in this package touches disk -
// see internal/services/filesystem for that.
package manifest

import (
	"sort"
	"strings"
)

// Workspace is an in-memory snapshot of every file discovered for one app,
// keyed by its virtual path (e.g. "base/kustomization.yaml" or
// "overlay/dev3/secrets.yaml"). Each value is either:
//   - map[string]any   a single parsed YAML document
//   - []any            a parsed multi-document YAML stream, or a JSON-patch array
//   - string           raw file content for anything that isn't YAML
//
// It is the direct equivalent of Ruby's Workflow::Models::AppManifestWorkspace.
type Workspace struct {
	AppName    string
	SourceEnv  string
	TargetEnvs []string
	Manifests  map[string]any
}

// New creates an empty workspace for the given app.
func New(appName, sourceEnv string, targetEnvs []string) *Workspace {
	return &Workspace{
		AppName:    appName,
		SourceEnv:  sourceEnv,
		TargetEnvs: targetEnvs,
		Manifests:  map[string]any{},
	}
}

// SortedPaths returns every manifest path in sorted order. Go's map
// iteration order is intentionally randomized, so anything that needs a
// stable, repeatable order (writing files, asserting in tests, or - if it
// ever happens inside a Temporal workflow function rather than an activity -
// driving a sequence of commands) should range over this instead of the map.
func (w *Workspace) SortedPaths() []string {
	paths := make([]string, 0, len(w.Manifests))
	for p := range w.Manifests {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// SourceOverlayFiles returns files under overlay/<source_env>/.
func (w *Workspace) SourceOverlayFiles() map[string]any {
	return w.filterPrefix("overlay/" + w.SourceEnv + "/")
}

// BaseFiles returns files under base/.
func (w *Workspace) BaseFiles() map[string]any {
	return w.filterPrefix("base/")
}

// TargetOverlayFiles returns files under overlay/<env>/.
func (w *Workspace) TargetOverlayFiles(env string) map[string]any {
	return w.filterPrefix("overlay/" + env + "/")
}

func (w *Workspace) filterPrefix(prefix string) map[string]any {
	out := map[string]any{}
	for path, content := range w.Manifests {
		if strings.HasPrefix(path, prefix) {
			out[path] = content
		}
	}
	return out
}
