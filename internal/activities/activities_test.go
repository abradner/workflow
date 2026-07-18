package activities_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/serviceclients/op"
	"github.com/abradner/workflow/internal/services/awssecrets"
	"github.com/abradner/workflow/internal/services/filesystem"
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
// actually be present in what it returns - only LoadKeycloakCredentials,
// called solely from SetupKeycloakEnvWorkflow, should ever carry it.
func TestLoadConfig_BlanksKeycloakAdminPassword(t *testing.T) {
	setRequiredConfigEnv(t, t.TempDir())

	a := &activities.Activities{}
	result, err := a.LoadConfig(context.Background())
	require.NoError(t, err)
	assert.Empty(t, result.Config.KeycloakAdmin, "LoadConfig must not expose the admin username either")
	assert.Empty(t, result.Config.KeycloakAdminPassword, "LoadConfig must not expose the admin password")

	creds, err := a.LoadKeycloakCredentials(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "super-secret-password", creds.AdminPassword, "the real password is still loadable via its own activity")
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

// fakeOpRunner captures the JSON payload piped to `op item create -`
// instead of running the real CLI.
type fakeOpRunner struct {
	lastPayload map[string]any
}

func (f *fakeOpRunner) Run(_ context.Context, _ string, _ []string, stdin []byte) (string, string, error) {
	if err := json.Unmarshal(stdin, &f.lastPayload); err != nil {
		return "", "", err
	}
	return "op_item_id", "", nil
}

func newSyncSecretsActivities(runner *fakeOpRunner) *activities.Activities {
	return &activities.Activities{
		AWSSecrets:  awssecrets.NewWithClient(fakeSecretsClient{}),
		OnePassword: op.NewWithRunner(runner),
	}
}

func fieldsFrom(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	raw, ok := payload["fields"].([]any)
	require.True(t, ok, "payload has no fields array: %#v", payload)

	fields := make([]map[string]any, len(raw))
	for i, f := range raw {
		fields[i] = f.(map[string]any)
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
	runner := &fakeOpRunner{}
	a := newSyncSecretsActivities(runner)

	result := executeSyncEnvSecrets(t, a, activities.SyncEnvSecretsInput{
		ProjectName: "pmn",
		SourceEnv:   "dev3",
		TargetEnv:   "dev4",
		KCPublicKey: "fresh_key",
	})
	assert.Equal(t, 1, result.SecretsExtracted)

	fields := fieldsFrom(t, runner.lastPayload)
	require.Len(t, fields, 1)
	assert.Equal(t, "mp.jwt.verify.publickey", fields[0]["label"])
	assert.Equal(t, "fresh_key", fields[0]["value"], "fresh Keycloak key must be injected")
}

// TestSyncEnvSecrets_LeavesValueAloneWithNoFreshKey covers the other half:
// KCPublicKey == "" (no reachable Keycloak for that environment) must leave
// the extracted value untouched rather than injecting an empty string.
func TestSyncEnvSecrets_LeavesValueAloneWithNoFreshKey(t *testing.T) {
	runner := &fakeOpRunner{}
	a := newSyncSecretsActivities(runner)

	executeSyncEnvSecrets(t, a, activities.SyncEnvSecretsInput{
		ProjectName: "pmn",
		SourceEnv:   "dev3",
		TargetEnv:   "dev5",
		KCPublicKey: "",
	})

	fields := fieldsFrom(t, runner.lastPayload)
	require.Len(t, fields, 1)
	assert.Equal(t, "stale", fields[0]["value"], "no fresh key available, so the extracted value is left alone")
}

// TestSyncEnvSecrets_DryRunSkipsIngestion proves DryRun still extracts and
// maps (so SecretsExtracted is accurate) but never calls the 1Password
// client at all.
func TestSyncEnvSecrets_DryRunSkipsIngestion(t *testing.T) {
	runner := &fakeOpRunner{}
	a := newSyncSecretsActivities(runner)

	result := executeSyncEnvSecrets(t, a, activities.SyncEnvSecretsInput{
		ProjectName: "pmn",
		SourceEnv:   "dev3",
		TargetEnv:   "dev4",
		DryRun:      true,
	})
	assert.Equal(t, 1, result.SecretsExtracted)
	assert.Nil(t, runner.lastPayload, "DryRun must never call the 1Password client")
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
