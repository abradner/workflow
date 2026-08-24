package activities_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/abradner/workflow/filesystem"
	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/serviceclients/op"
	"github.com/abradner/workflow/internal/serviceclients/op/optest"
	"github.com/abradner/workflow/internal/services/awssecrets"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// setRequiredConfigEnv sets every env var config.Load requires, so tests
// that need a real (non-mocked) Load call don't fail on unrelated missing
// vars.
func setRequiredConfigEnv(t *testing.T, dir string) {
	t.Helper()
	vars := map[string]string{
		"SOURCE_DIR":              dir,
		"DEST_DIR":                dir,
		"CLUSTER_APPS_DIR":        dir,
		"TALOS_TEMPLATE_DIR":      dir,
		"SOURCE_ENV":              "dev3",
		"TARGET_ENVS":             "dev4",
		"APP_PATTERN":             "wtf-*",
		"PROJECT_NAME":            "wtf",
		"TLD":                     "f-ck.xyz",
		"REPO_URL":                "https://example.com/repo.git",
		"REGISTRY_HOSTNAME":       "cr.infra.fqdn",
		"REGISTRY_1P_ITEM_ID":     "12345",
		"KEYCLOAK_ADMIN_PASSWORD": "super-secret-password",
	}
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

// TestLoadConfig_BlanksKeycloakAdminPassword is the regression test for the
// P1 fix that keeps the Keycloak admin password out of every workflow's
// Temporal history: LoadConfig's result is recorded verbatim in whichever
// workflow calls it (every one of the five), so the password must never
// actually be present in what it returns. RunKeycloakSetup loads it
// directly via its own config.Load() call instead (see its doc comment) -
// config.Load() itself returning the real value is covered by
// internal/config/config_test.go.
func TestLoadConfig_BlanksKeycloakAdminPassword(t *testing.T) {
	setRequiredConfigEnv(t, t.TempDir())

	a := &activities.Activities{}
	result, err := a.LoadConfig(context.Background())
	require.NoError(t, err)
	assert.Empty(t, result.Config.KeycloakAdmin, "LoadConfig must not expose the admin username either")
	assert.Empty(t, result.Config.KeycloakAdminPassword, "LoadConfig must not expose the admin password")
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
	require.Equal(t, 2, result.FilesWritten, "base/ingress.yaml + the injected registry-pull-secret.yaml")

	content, err := os.ReadFile(filepath.Join(cfg.DestDir, "wtf-core/base/ingress.yaml"))
	require.NoError(t, err, "BuildAppFiles must write the file itself, not just return its content")
	assert.Contains(t, string(content), "port: 8080\n", "port must render as an integer, not 8080.0")
	assert.NotContains(t, string(content), "8080.0")
}

// TestBuildAppFiles_DryRunWritesNothing proves DryRun still reports an
// accurate count without touching disk.
func TestBuildAppFiles_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "wtf-core/base/deployment.yaml"), "kind: Deployment\n")

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
	result, err := a.BuildAppFiles(context.Background(), activities.BuildAppFilesInput{Config: cfg, AppName: "wtf-core", DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, 2, result.FilesWritten, "base/deployment.yaml + the injected registry-pull-secret.yaml")

	_, statErr := os.Stat(filepath.Join(cfg.DestDir, "wtf-core/base/deployment.yaml"))
	assert.True(t, os.IsNotExist(statErr), "DryRun must not write anything")
}

// fakeSecretsClient serves one fixed secret regardless of which environment
// is asked for - enough to exercise SyncEnvSecrets's own extract-map-ingest
// pipeline without needing the full pagination behavior awssecrets already
// has its own tests for.
type fakeSecretsClient struct{}

func (fakeSecretsClient) ListSecrets(_ context.Context, _ *secretsmanager.ListSecretsInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	return &secretsmanager.ListSecretsOutput{SecretList: []types.SecretListEntry{
		{Name: aws.String("dev3/pmn-ui-api-config")},
	}}, nil
}

func (fakeSecretsClient) GetSecretValue(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return &secretsmanager.GetSecretValueOutput{
		SecretString: aws.String(`{"mp.jwt.verify.publickey":"stale"}`),
	}, nil
}

// newSyncSecretsActivities wires SyncEnvSecrets against the contract fake
// rather than a permissive stub. The upsert reads before it writes, so the
// fake has to behave like a vault - answer a get with what a create stored,
// and reject an edit payload the real CLI would reject - or the test proves
// nothing about the round trip it exists to exercise.
func newSyncSecretsActivities(runner *optest.Runner) *activities.Activities {
	return &activities.Activities{
		AWSSecrets:  awssecrets.NewWithClient(fakeSecretsClient{}),
		OnePassword: op.NewWithRunner(runner),
	}
}

// fieldsFrom reads the fields of the single item the fake vault holds.
func fieldsFrom(t *testing.T, runner *optest.Runner) []map[string]any {
	t.Helper()
	item := runner.Last()
	require.NotNil(t, item, "nothing was written to the vault")

	fields := make([]map[string]any, 0, len(item.Fields))
	for _, f := range item.Fields {
		m, ok := f.(map[string]any)
		require.True(t, ok, "unexpected field shape: %#v", f)
		fields = append(fields, m)
	}
	return fields
}

// executeSyncEnvSecrets runs SyncEnvSecrets through a real Temporal activity
// environment rather than calling it directly - it uses activity.GetLogger
// internally (via OnePasswordSamlKeyInjector.Logger), which panics without a
// genuine activity execution context around it.
func executeSyncEnvSecrets(t *testing.T, a *activities.Activities, in activities.SyncEnvSecretsInput) activities.SyncEnvSecretsResult {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	activityEnv := suite.NewTestActivityEnvironment()
	activityEnv.RegisterActivity(a.SyncEnvSecrets)

	encoded, err := activityEnv.ExecuteActivity(a.SyncEnvSecrets, in)
	require.NoError(t, err)

	var result activities.SyncEnvSecretsResult
	require.NoError(t, encoded.Get(&result))
	return result
}

// TestSyncEnvSecrets_InjectsFreshKeycloakPublicKey is the regression test
// for keeping the extract-map-ingest pipeline bundled into one activity
// (see the doc comment on SyncEnvSecrets): the actual secret mapping logic
// - renaming the environment segment and injecting a fresh SAML public key
// - only runs inside this activity now, so it needs its own coverage here
// rather than at the workflow level.
func TestSyncEnvSecrets_InjectsFreshKeycloakPublicKey(t *testing.T) {
	runner := &optest.Runner{}
	a := newSyncSecretsActivities(runner)

	result := executeSyncEnvSecrets(t, a, activities.SyncEnvSecretsInput{
		VaultName:   "Tooling",
		ProjectName: "pmn",
		SourceEnv:   "dev3",
		TargetEnv:   "dev4",
		KCPublicKey: "fresh_key",
	})
	assert.Equal(t, 1, result.SecretsExtracted)

	fields := fieldsFrom(t, runner)
	require.Len(t, fields, 1)
	assert.Equal(t, "mp.jwt.verify.publickey", fields[0]["label"])
	assert.Equal(t, "fresh_key", fields[0]["value"], "fresh Keycloak key must be injected")
}

// TestSyncEnvSecrets_LeavesValueAloneWithNoFreshKey covers the other half:
// KCPublicKey == "" (no reachable Keycloak for that environment) must leave
// the extracted value untouched rather than injecting an empty string.
func TestSyncEnvSecrets_LeavesValueAloneWithNoFreshKey(t *testing.T) {
	runner := &optest.Runner{}
	a := newSyncSecretsActivities(runner)

	executeSyncEnvSecrets(t, a, activities.SyncEnvSecretsInput{
		VaultName:   "Tooling",
		ProjectName: "pmn",
		SourceEnv:   "dev3",
		TargetEnv:   "dev5",
		KCPublicKey: "",
	})

	fields := fieldsFrom(t, runner)
	require.Len(t, fields, 1)
	assert.Equal(t, "stale", fields[0]["value"], "no fresh key available, so the extracted value is left alone")
}

// TestSyncEnvSecrets_DryRunSkipsIngestion proves DryRun still extracts and
// maps (so SecretsExtracted is accurate) but never calls the 1Password
// client at all.
func TestSyncEnvSecrets_DryRunSkipsIngestion(t *testing.T) {
	runner := &optest.Runner{}
	a := newSyncSecretsActivities(runner)

	result := executeSyncEnvSecrets(t, a, activities.SyncEnvSecretsInput{
		VaultName:   "Tooling",
		ProjectName: "pmn",
		SourceEnv:   "dev3",
		TargetEnv:   "dev4",
		DryRun:      true,
	})
	assert.Equal(t, 1, result.SecretsExtracted)
	assert.Empty(t, runner.Items, "DryRun must never call the 1Password client")
	assert.Empty(t, runner.Calls, "not even a read")
}

// fakeOpNoteRunner serves a fixed Secure Note body for `op item get`,
// regardless of item ID - enough to exercise RenderTalosTemplates without a
// real `op` binary or 1Password vault.
type fakeOpNoteRunner struct {
	noteContent string
}

func (f *fakeOpNoteRunner) Run(_ context.Context, _ string, _ []string, _ []byte) (string, string, error) {
	return f.noteContent, "", nil
}

// TestRenderTalosTemplates_RendersAndWritesWhenPlaceholdersResolve is the
// regression test for keeping the Secure Note content and rendered
// (secret-bearing) files inside one activity: see the doc comment on
// RenderTalosTemplates. Its result type has no field to carry that content
// even if this test wanted to check one - the only way to verify rendering
// actually happened is to read the file this activity wrote itself.
func TestRenderTalosTemplates_RendersAndWritesWhenPlaceholdersResolve(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.template.yaml"), "id: {{ cluster.id }}\n")

	a := &activities.Activities{
		FS:          filesystem.New(),
		OnePassword: op.NewWithRunner(&fakeOpNoteRunner{noteContent: "cluster:\n  id: abc123\n"}),
	}

	result, err := a.RenderTalosTemplates(context.Background(), activities.RenderTalosTemplatesInput{
		ItemID:      "item-123",
		TemplateDir: dir,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, result.SecretKeysLoaded)
	assert.Equal(t, 1, result.TemplatesRendered)

	content, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	require.NoError(t, err, "RenderTalosTemplates must write the rendered file itself")
	assert.Equal(t, "id: abc123\n", string(content))
}

// TestRenderTalosTemplates_DryRunWritesNothing proves DryRun still renders
// (so the result counts are accurate) but never touches disk.
func TestRenderTalosTemplates_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.template.yaml"), "id: {{ cluster.id }}\n")

	a := &activities.Activities{
		FS:          filesystem.New(),
		OnePassword: op.NewWithRunner(&fakeOpNoteRunner{noteContent: "cluster:\n  id: abc123\n"}),
	}

	result, err := a.RenderTalosTemplates(context.Background(), activities.RenderTalosTemplatesInput{
		ItemID:      "item-123",
		TemplateDir: dir,
		DryRun:      true,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, result.TemplatesRendered)

	_, statErr := os.Stat(filepath.Join(dir, "config.yaml"))
	assert.True(t, os.IsNotExist(statErr), "DryRun must not write the rendered file")
}

// TestRenderTalosTemplates_FailsWithUnresolvedPlaceholders proves an
// unresolved placeholder fails before anything is written.
func TestRenderTalosTemplates_FailsWithUnresolvedPlaceholders(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.template.yaml"), "id: {{ cluster.id }}\ntoken: {{ missing.token }}\n")

	a := &activities.Activities{
		FS:          filesystem.New(),
		OnePassword: op.NewWithRunner(&fakeOpNoteRunner{noteContent: "cluster:\n  id: abc123\n"}),
	}

	_, err := a.RenderTalosTemplates(context.Background(), activities.RenderTalosTemplatesInput{
		ItemID:      "item-123",
		TemplateDir: dir,
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "unresolved placeholder")

	_, statErr := os.Stat(filepath.Join(dir, "config.yaml"))
	assert.True(t, os.IsNotExist(statErr), "must not write anything if placeholders are unresolved")
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

// The upsert's defining property: a second run against an existing item edits
// it in place, preserving the vault's own field IDs, rather than creating a
// second item beside it.
func TestSyncEnvSecrets_UpsertsInsteadOfCreatingASecondItem(t *testing.T) {
	runner := &optest.Runner{}
	a := newSyncSecretsActivities(runner)
	in := activities.SyncEnvSecretsInput{
		VaultName: "Tooling", ProjectName: "pmn",
		SourceEnv: "dev3", TargetEnv: "dev4", KCPublicKey: "fresh_key",
	}

	first := executeSyncEnvSecrets(t, a, in)
	assert.True(t, first.ItemCreated, "first run has nothing to update")
	require.Len(t, runner.Items, 1)
	idAfterCreate := runner.Items[0].ID
	fieldIDAfterCreate := fieldsFrom(t, runner)[0]["id"]

	second := executeSyncEnvSecrets(t, a, in)
	assert.False(t, second.ItemCreated, "second run must edit, not create")
	require.Len(t, runner.Items, 1, "a second item beside the first is the bug this replaces")
	assert.Equal(t, idAfterCreate, runner.Items[0].ID)
	assert.Equal(t, fieldIDAfterCreate, fieldsFrom(t, runner)[0]["id"],
		"the vault's field ID must survive the update")
}

// A field the vault holds that this run did not write is preserved and
// counted. Under op item edit's REPLACE semantics, omitting it would delete it.
func TestSyncEnvSecrets_PreservesAndCountsStaleFields(t *testing.T) {
	runner := &optest.Runner{}
	runner.Add(&optest.Item{
		ID: "existing", Title: "k8s-pmn-dev4", Category: "SECURE_NOTE", Vault: "Tooling",
		Fields: []any{map[string]any{
			"id": "hand-added", "section": map[string]any{"id": "manual"},
			"label": "added-by-a-human", "value": "keep me", "type": "CONCEALED",
		}},
	})
	a := newSyncSecretsActivities(runner)

	result := executeSyncEnvSecrets(t, a, activities.SyncEnvSecretsInput{
		VaultName: "Tooling", ProjectName: "pmn",
		SourceEnv: "dev3", TargetEnv: "dev4", KCPublicKey: "fresh_key",
	})

	assert.False(t, result.ItemCreated)
	assert.Equal(t, 1, result.StaleFields)

	var kept bool
	for _, f := range fieldsFrom(t, runner) {
		if f["id"] == "hand-added" {
			kept = true
			assert.Equal(t, "keep me", f["value"])
		}
	}
	assert.True(t, kept, "sync-1p must not delete what it did not write")
}

// With Prune set, a field the vault holds that this run did not write is
// removed. This is the only path in the tool that deletes vault data.
func TestSyncEnvSecrets_PruneRemovesStaleFields(t *testing.T) {
	runner := &optest.Runner{}
	runner.Add(&optest.Item{
		ID: "existing", Title: "k8s-pmn-dev4", Category: "SECURE_NOTE", Vault: "Tooling",
		Fields: []any{map[string]any{
			"id": "hand-added", "section": map[string]any{"id": "manual"},
			"label": "added-by-a-human", "value": "delete me", "type": "CONCEALED",
		}},
	})
	a := newSyncSecretsActivities(runner)

	result := executeSyncEnvSecrets(t, a, activities.SyncEnvSecretsInput{
		VaultName: "Tooling", ProjectName: "pmn",
		SourceEnv: "dev3", TargetEnv: "dev4", KCPublicKey: "fresh_key",
		Prune: true,
	})

	assert.Equal(t, 1, result.StaleFields)
	for _, f := range fieldsFrom(t, runner) {
		assert.NotEqual(t, "hand-added", f["id"], "prune must remove the stale field")
	}
}
