package activities_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/services/filesystem"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestDiscoverApps(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "wtf-core"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "wtf-ext"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "other-app"), 0o755))

	a := &activities.Activities{FS: filesystem.New()}
	result, err := a.DiscoverApps(context.Background(), activities.DiscoverAppsInput{
		Config: config.Config{SourceDir: dir, AppPattern: "wtf-*"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"wtf-core", "wtf-ext"}, result.Apps)
}

// TestBuildAppFiles_PreservesIntegerFields is the regression test for the
// exact bug BuildAppFiles's design avoids: bundling extract+transform+render
// into one activity so a value like a port number never round-trips through
// Temporal's JSON activity boundary as a bare map[string]any (which would
// silently turn 8080 into 8080.0 in the rendered YAML).
func TestBuildAppFiles_PreservesIntegerFields(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "wtf-core/base/ingress.yaml"), `
kind: Ingress
spec:
  rules:
    - host: app.example.com
      http:
        paths:
          - backend:
              service:
                name: app-svc
                port:
                  number: 8080
`)

	cfg := config.Config{
		SourceDir:                 dir,
		DestDir:                   filepath.Join(dir, "dest"),
		SourceEnv:                 "dev3",
		Environments:              []string{"dev3"},
		ProjectName:               "wtf",
		TLD:                       "f-ck.xyz",
		ExternalSecretsAPIVersion: "external-secrets.io/v1",
		RegistryHostname:          "cr.infra.fqdn",
		Registry1PItemID:          "12345",
	}

	a := &activities.Activities{FS: filesystem.New()}
	result, err := a.BuildAppFiles(context.Background(), activities.BuildAppFilesInput{Config: cfg, AppName: "wtf-core"})
	require.NoError(t, err)

	var ingressFile *activities.FileWrite
	for i := range result.Files {
		if result.Files[i].Path == filepath.Join(cfg.DestDir, "wtf-core/base/ingress.yaml") {
			ingressFile = &result.Files[i]
		}
	}
	require.NotNil(t, ingressFile, "expected base/ingress.yaml among the built files")
	assert.Contains(t, ingressFile.Content, "port: 8080\n", "port must render as an integer, not 8080.0")
	assert.NotContains(t, ingressFile.Content, "8080.0")
}

func TestWriteFiles_CreatesDirectoriesAndWritesContent(t *testing.T) {
	dir := t.TempDir()
	a := &activities.Activities{FS: filesystem.New()}

	err := a.WriteFiles(context.Background(), activities.WriteFilesInput{
		Files: []activities.FileWrite{
			{Path: filepath.Join(dir, "a/b/c.yaml"), Content: "kind: Foo\n"},
		},
	})
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(dir, "a/b/c.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "kind: Foo\n", string(got))
}
