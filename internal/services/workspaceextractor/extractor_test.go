package workspaceextractor_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/services/workspaceextractor"
)

type fakeFS struct {
	dirs    map[string]bool
	entries map[string][]string
	yaml    map[string]any
	raw     map[string]string
}

func (f *fakeFS) DirectoryExists(path string) bool { return f.dirs[path] }

func (f *fakeFS) PathEntries(path string) ([]string, error) { return f.entries[path], nil }

func (f *fakeFS) IsYAML(path string) bool {
	return len(path) > 5 && path[len(path)-5:] == ".yaml"
}

func (f *fakeFS) ReadYAML(path string) (any, error) { return f.yaml[path], nil }

func (f *fakeFS) ReadFile(path string) (string, error) { return f.raw[path], nil }

func TestExtract_LoadsBaseAndSourceOverlayIntoVirtualPaths(t *testing.T) {
	fs := &fakeFS{
		dirs: map[string]bool{
			"/src/wtf-core/base":         true,
			"/src/wtf-core/overlay/dev3": true,
			"/src/wtf-core/base/nested":  true,
		},
		entries: map[string][]string{
			"/src/wtf-core/base":         {"/src/wtf-core/base/kustomization.yaml", "/src/wtf-core/base/nested"},
			"/src/wtf-core/base/nested":  {"/src/wtf-core/base/nested/extra.yaml"},
			"/src/wtf-core/overlay/dev3": {"/src/wtf-core/overlay/dev3/secrets.yaml", "/src/wtf-core/overlay/dev3/notes.txt"},
		},
		yaml: map[string]any{
			"/src/wtf-core/base/kustomization.yaml": map[string]any{"kind": "Kustomization"},
			"/src/wtf-core/base/nested/extra.yaml":  map[string]any{"kind": "Extra"},
			"/src/wtf-core/overlay/dev3/secrets.yaml": []any{
				map[string]any{"op": "replace"},
			},
		},
		raw: map[string]string{
			"/src/wtf-core/overlay/dev3/notes.txt": "plain text content",
		},
	}

	extractor := workspaceextractor.New("/src", "dev3", []string{"dev4"}, fs)
	ws, err := extractor.Extract("wtf-core")
	require.NoError(t, err)

	assert.Equal(t, "wtf-core", ws.AppName)
	assert.Equal(t, map[string]any{"kind": "Kustomization"}, ws.Manifests["base/kustomization.yaml"])
	assert.Equal(t, map[string]any{"kind": "Extra"}, ws.Manifests["base/nested/extra.yaml"])
	assert.Equal(t, []any{map[string]any{"op": "replace"}}, ws.Manifests["overlay/dev3/secrets.yaml"])
	assert.Equal(t, "plain text content", ws.Manifests["overlay/dev3/notes.txt"])
}

func TestExtract_SkipsMissingDirectories(t *testing.T) {
	fs := &fakeFS{dirs: map[string]bool{}}

	extractor := workspaceextractor.New("/src", "dev3", []string{"dev4"}, fs)
	ws, err := extractor.Extract("wtf-missing")
	require.NoError(t, err)
	assert.Empty(t, ws.Manifests)
}
