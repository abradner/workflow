package filesystem_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/services/filesystem"
)

func TestListDirectories(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, filesystem.New().CreateDirectory(filepath.Join(dir, "wtf-core")))
	require.NoError(t, filesystem.New().CreateDirectory(filepath.Join(dir, "wtf-ext")))
	require.NoError(t, filesystem.New().WriteFile(filepath.Join(dir, "wtf-notadir"), "not a directory"))

	fs := filesystem.New()
	dirs, err := fs.ListDirectories(dir, "wtf-*")
	require.NoError(t, err)

	names := make([]string, len(dirs))
	for i, d := range dirs {
		names[i] = fs.BaseFilename(d)
	}
	assert.Equal(t, []string{"wtf-core", "wtf-ext"}, names)
}

func TestListDirectories_MissingSourceDir(t *testing.T) {
	_, err := filesystem.New().ListDirectories("/does/not/exist", "*")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestReadYAML_RoundTripsThroughWriteFile(t *testing.T) {
	dir := t.TempDir()
	fs := filesystem.New()
	path := filepath.Join(dir, "deployment.yaml")

	require.NoError(t, fs.WriteFile(path, "kind: Deployment\nspec:\n  replicas: 3\n"))

	doc, err := fs.ReadYAML(path)
	require.NoError(t, err)

	m, ok := doc.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Deployment", m["kind"])
	spec, ok := m["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 3, spec["replicas"])
}

func TestIsYAML(t *testing.T) {
	fs := filesystem.New()
	assert.True(t, fs.IsYAML("foo.yaml"))
	assert.True(t, fs.IsYAML("foo.YML"))
	assert.False(t, fs.IsYAML("foo.txt"))
}
