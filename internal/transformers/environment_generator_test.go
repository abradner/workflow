package transformers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/manifest"
	"github.com/abradner/workflow/internal/transformers"
)

func TestEnvironmentGenerator(t *testing.T) {
	t.Run("clones source overlay files into every other target env, rewriting env references", func(t *testing.T) {
		ws := manifest.New("test-app", "dev3", []string{"dev4", "dev5"})
		ws.Manifests["base/deployment.yaml"] = map[string]any{"kind": "Deployment"}
		ws.Manifests["overlay/dev3/kustomization.yaml"] = map[string]any{
			"namespace": "wtf-dev3",
			"nested":    map[string]any{"note": "points at dev3 stuff"},
			"list":      []any{"dev3-a", "dev3-b"},
		}

		out := transformers.EnvironmentGenerator(ws)

		require.Contains(t, out.Manifests, "overlay/dev4/kustomization.yaml")
		require.Contains(t, out.Manifests, "overlay/dev5/kustomization.yaml")

		dev4 := out.Manifests["overlay/dev4/kustomization.yaml"].(map[string]any)
		assert.Equal(t, "wtf-dev4", dev4["namespace"])
		assert.Equal(t, map[string]any{"note": "points at dev4 stuff"}, dev4["nested"])
		assert.Equal(t, []any{"dev4-a", "dev4-b"}, dev4["list"])

		// The source env itself is dropped since it wasn't requested as a
		// deployment target.
		assert.NotContains(t, out.Manifests, "overlay/dev3/kustomization.yaml")
		// base/ files are untouched by this transformer.
		assert.Contains(t, out.Manifests, "base/deployment.yaml")
	})

	t.Run("keeps the source overlay when it is itself a deployment target", func(t *testing.T) {
		ws := manifest.New("test-app", "dev3", []string{"dev3", "dev4"})
		ws.Manifests["overlay/dev3/kustomization.yaml"] = map[string]any{"namespace": "wtf-dev3"}

		out := transformers.EnvironmentGenerator(ws)

		assert.Contains(t, out.Manifests, "overlay/dev3/kustomization.yaml")
		assert.Contains(t, out.Manifests, "overlay/dev4/kustomization.yaml")
	})

	t.Run("does not clone base/ files", func(t *testing.T) {
		ws := manifest.New("test-app", "dev3", []string{"dev4"})
		ws.Manifests["base/deployment.yaml"] = map[string]any{"kind": "Deployment"}

		out := transformers.EnvironmentGenerator(ws)

		assert.Len(t, out.Manifests, 1)
		assert.Contains(t, out.Manifests, "base/deployment.yaml")
	})
}
