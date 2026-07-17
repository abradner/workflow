package manifest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/manifest"
)

func TestWorkspace_Filters(t *testing.T) {
	ws := manifest.New("app", "dev3", []string{"dev4"})
	ws.Manifests["base/deployment.yaml"] = "base"
	ws.Manifests["overlay/dev3/secrets.yaml"] = "dev3-secrets"
	ws.Manifests["overlay/dev4/secrets.yaml"] = "dev4-secrets"

	assert.Equal(t, map[string]any{"base/deployment.yaml": "base"}, ws.BaseFiles())
	assert.Equal(t, map[string]any{"overlay/dev3/secrets.yaml": "dev3-secrets"}, ws.SourceOverlayFiles())
	assert.Equal(t, map[string]any{"overlay/dev4/secrets.yaml": "dev4-secrets"}, ws.TargetOverlayFiles("dev4"))

	assert.Equal(t, []string{"base/deployment.yaml", "overlay/dev3/secrets.yaml", "overlay/dev4/secrets.yaml"}, ws.SortedPaths())
}

func TestDig(t *testing.T) {
	doc := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{"replicas": 3},
			},
		},
	}

	assert.Equal(t, 3, manifest.Dig(doc, "spec", "template", "spec", "replicas"))
	assert.Nil(t, manifest.Dig(doc, "spec", "missing", "spec"))
	assert.Nil(t, manifest.Dig(doc, "spec", "template", "spec", "replicas", "too", "deep"))

	assert.Equal(t, map[string]any{"replicas": 3}, manifest.DigMap(doc, "spec", "template", "spec"))
	assert.Nil(t, manifest.DigMap(doc, "nope"))

	strDoc := map[string]any{"metadata": map[string]any{"name": "app"}}
	assert.Equal(t, "app", manifest.DigString(strDoc, "metadata", "name"))
	assert.Equal(t, "", manifest.DigString(strDoc, "metadata", "missing"))

	sliceDoc := map[string]any{"list": []any{"a", "b"}}
	assert.Equal(t, []any{"a", "b"}, manifest.DigSlice(sliceDoc, "list"))
}

func TestMutateYAML(t *testing.T) {
	addMarker := func(d any) any {
		m, ok := d.(map[string]any)
		if !ok {
			return d
		}
		m["touched"] = true
		return m
	}

	t.Run("single document", func(t *testing.T) {
		result := manifest.MutateYAML(map[string]any{"kind": "Foo"}, addMarker)
		assert.Equal(t, map[string]any{"kind": "Foo", "touched": true}, result)
	})

	t.Run("document stream", func(t *testing.T) {
		result := manifest.MutateYAML([]any{map[string]any{"kind": "A"}, map[string]any{"kind": "B"}}, addMarker)
		docs := result.([]any)
		require.Len(t, docs, 2)
		assert.Equal(t, true, docs[0].(map[string]any)["touched"])
		assert.Equal(t, true, docs[1].(map[string]any)["touched"])
	})

	t.Run("passthrough for anything else", func(t *testing.T) {
		assert.Equal(t, "raw string", manifest.MutateYAML("raw string", addMarker))
	})
}

func TestExtractEnv(t *testing.T) {
	assert.Equal(t, "dev4", manifest.ExtractEnv("overlay/dev4/secrets.yaml"))
	assert.Equal(t, "", manifest.ExtractEnv("base/secrets.yaml"))
}

func TestIsYAMLPath(t *testing.T) {
	assert.True(t, manifest.IsYAMLPath("foo.yaml"))
	assert.True(t, manifest.IsYAMLPath("foo.YML"))
	assert.False(t, manifest.IsYAMLPath("foo.txt"))
}

func TestRenderYAML(t *testing.T) {
	t.Run("single document has no leading '---'", func(t *testing.T) {
		out, err := manifest.RenderYAML(map[string]any{"kind": "Foo"})
		require.NoError(t, err)
		assert.Equal(t, "kind: Foo\n", out)
	})

	t.Run("a JSON-patch array is one YAML sequence, not a stream", func(t *testing.T) {
		out, err := manifest.RenderYAML([]any{map[string]any{"op": "replace", "path": "/x", "value": "y"}})
		require.NoError(t, err)
		assert.NotContains(t, out, "---")
	})

	t.Run("a document stream (elements with 'kind') is '---'-separated", func(t *testing.T) {
		out, err := manifest.RenderYAML([]any{
			map[string]any{"kind": "A"},
			map[string]any{"kind": "B"},
		})
		require.NoError(t, err)
		assert.Equal(t, "kind: A\n---\nkind: B\n", out)
	})
}
