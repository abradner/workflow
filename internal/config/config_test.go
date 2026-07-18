package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/config"
)

func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()

	setEnv(t, map[string]string{
		"SOURCE_DIR":          filepath.Join(dir, "source"),
		"DEST_DIR":            filepath.Join(dir, "dest"),
		"CLUSTER_APPS_DIR":    filepath.Join(dir, "cluster-apps"),
		"TALOS_TEMPLATE_DIR":  filepath.Join(dir, "talos"),
		"SOURCE_ENV":          "dev3",
		"TARGET_ENVS":         " dev4 , dev5 ",
		"APP_PATTERN":         "wtf-*",
		"PROJECT_NAME":        "wtf",
		"TLD":                 "f-ck.xyz",
		"REPO_URL":            "https://example.com/repo.git",
		"REGISTRY_HOSTNAME":   "cr.infra.fqdn",
		"REGISTRY_1P_ITEM_ID": "12345",
	})

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "source"), cfg.SourceDir)
	assert.Equal(t, []string{"dev4", "dev5"}, cfg.Environments, "whitespace around each env should be trimmed")
	assert.Equal(t, "external-secrets.io/v1", cfg.ExternalSecretsAPIVersion)
	assert.Equal(t, "admin", cfg.KeycloakAdmin, "defaults to admin when unset")
	assert.Equal(t, "admin", cfg.KeycloakAdminPassword)
	assert.Empty(t, cfg.TalosItemID, "optional field with no default")
}

func TestLoad_MissingRequiredVar(t *testing.T) {
	setEnv(t, map[string]string{
		"DEST_DIR":            "/dest",
		"CLUSTER_APPS_DIR":    "/cluster-apps",
		"TALOS_TEMPLATE_DIR":  "/talos",
		"SOURCE_ENV":          "dev3",
		"TARGET_ENVS":         "dev4",
		"APP_PATTERN":         "wtf-*",
		"PROJECT_NAME":        "wtf",
		"TLD":                 "f-ck.xyz",
		"REPO_URL":            "https://example.com/repo.git",
		"REGISTRY_HOSTNAME":   "cr.infra.fqdn",
		"REGISTRY_1P_ITEM_ID": "12345",
	})
	// SOURCE_DIR deliberately left unset - make sure it really is, in case
	// the ambient test process environment happens to carry one.
	original, wasSet := os.LookupEnv("SOURCE_DIR")
	require.NoError(t, os.Unsetenv("SOURCE_DIR"))
	t.Cleanup(func() {
		if wasSet {
			os.Setenv("SOURCE_DIR", original)
		}
	})

	_, err := config.Load()
	require.Error(t, err)
}

func TestLoad_ExpandsRelativePaths(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(wd) })

	setEnv(t, map[string]string{
		"SOURCE_DIR":          "relative-source",
		"DEST_DIR":            "relative-dest",
		"CLUSTER_APPS_DIR":    "relative-cluster",
		"TALOS_TEMPLATE_DIR":  "relative-talos",
		"SOURCE_ENV":          "dev3",
		"TARGET_ENVS":         "dev4",
		"APP_PATTERN":         "wtf-*",
		"PROJECT_NAME":        "wtf",
		"TLD":                 "f-ck.xyz",
		"REPO_URL":            "https://example.com/repo.git",
		"REGISTRY_HOSTNAME":   "cr.infra.fqdn",
		"REGISTRY_1P_ITEM_ID": "12345",
	})

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(cfg.SourceDir))
	assert.Equal(t, filepath.Join(dir, "relative-source"), cfg.SourceDir)
}
