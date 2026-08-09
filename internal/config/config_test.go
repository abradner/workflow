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
	// EvalSymlinks, not t.TempDir() directly: on macOS TempDir hands back a
	// path under /var, which is a symlink to /private/var. Load resolves
	// relative paths with filepath.Abs, which consults os.Getwd(), and after
	// the Chdir below that returns the *resolved* /private/var form. Comparing
	// the unresolved expectation against the resolved actual fails on macOS
	// while passing on Linux, where /tmp is not symlinked.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
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

func TestLoad_ExpandsAdditionalExactSecrets(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(wd) })

	setEnv(t, map[string]string{
		"SOURCE_DIR": dir, "DEST_DIR": dir, "CLUSTER_APPS_DIR": dir, "TALOS_TEMPLATE_DIR": dir,
		"SOURCE_ENV": "dev3", "TARGET_ENVS": "dev4", "APP_PATTERN": "wtf-*",
		"PROJECT_NAME": "wtf", "TLD": "f-ck.xyz", "REPO_URL": "https://example.com/repo.git",
		"REGISTRY_HOSTNAME": "cr.infra.fqdn", "REGISTRY_1P_ITEM_ID": "12345",
		// trailing empty entry and surrounding spaces are both realistic
		"ADDITIONAL_EXACT_SECRETS": " dev/cache/pmn-{{source_env}}-ro , dev/cache/pmn-{{source_env}}-rw ,",
	})

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"dev/cache/pmn-dev3-ro", "dev/cache/pmn-dev3-rw"}, cfg.AdditionalExactSecrets)
}

func TestLoad_AdditionalExactSecretsIsOptional(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(wd) })

	setEnv(t, map[string]string{
		"SOURCE_DIR": dir, "DEST_DIR": dir, "CLUSTER_APPS_DIR": dir, "TALOS_TEMPLATE_DIR": dir,
		"SOURCE_ENV": "dev3", "TARGET_ENVS": "dev4", "APP_PATTERN": "wtf-*",
		"PROJECT_NAME": "wtf", "TLD": "f-ck.xyz", "REPO_URL": "https://example.com/repo.git",
		"REGISTRY_HOSTNAME": "cr.infra.fqdn", "REGISTRY_1P_ITEM_ID": "12345",
	})

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.AdditionalExactSecrets)
}
