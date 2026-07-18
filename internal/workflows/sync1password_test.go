package workflows_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/domain"
	"github.com/abradner/workflow/internal/workflows"
)

func TestSync1PasswordWorkflow_FansOutOneChildPerEnvironment(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(workflows.Sync1PasswordEnvWorkflow)
	var a *activities.Activities

	cfg := config.Config{
		SourceEnv:    "dev4",
		Environments: []string{"dev4", "dev5"},
		ProjectName:  "pmn",
		TLD:          "f-ck.xyz",
	}

	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.FetchSamlCredentials, mock.Anything, mock.MatchedBy(func(in activities.FetchSamlCredentialsInput) bool {
		return in.BaseURL == "https://pmn-keycloak.pmn.dev4.f-ck.xyz"
	})).Return(activities.FetchSamlCredentialsResult{Credentials: &domain.SamlCredentials{PublicKey: "fresh_key", SSOXML: "<xml/>"}}, nil)

	env.OnActivity(a.FetchSamlCredentials, mock.Anything, mock.MatchedBy(func(in activities.FetchSamlCredentialsInput) bool {
		return in.BaseURL == "https://pmn-keycloak.pmn.dev5.f-ck.xyz"
	})).Return(activities.FetchSamlCredentialsResult{Credentials: nil}, nil)

	// SyncEnvSecrets bundles extraction, mapping, and ingestion into one
	// activity specifically so the actual secret values never appear as an
	// activity result or workflow input - see its doc comment in
	// internal/activities/activities.go. That means this workflow-level
	// test can only assert on what gets passed *into* it (each
	// environment's own KCPublicKey, correctly PEM-formatted where a fresh
	// key was found) - the actual mapping/injection behavior has its own
	// coverage in internal/activities/activities_test.go.
	var kcPublicKeys = map[string]string{}
	env.OnActivity(a.SyncEnvSecrets, mock.Anything, mock.MatchedBy(func(in activities.SyncEnvSecretsInput) bool {
		kcPublicKeys[in.TargetEnv] = in.KCPublicKey
		return in.ProjectName == "pmn" && in.SourceEnv == "dev4"
	})).Return(activities.SyncEnvSecretsResult{SecretsExtracted: 1}, nil)

	env.ExecuteWorkflow(workflows.Sync1PasswordWorkflow, workflows.Sync1PasswordInput{DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.Sync1PasswordResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 1, result.SecretsExtracted)
	require.Equal(t, 2, result.EnvironmentsSynced)

	expectedPEM := domain.SamlCredentials{PublicKey: "fresh_key"}.PEMPublicKey()
	require.Equal(t, expectedPEM, kcPublicKeys["dev4"], "dev4 has fresh Keycloak credentials, PEM-formatted")
	require.Equal(t, "", kcPublicKeys["dev5"], "dev5 had no reachable Keycloak, so no key is passed")
}

func TestSync1PasswordWorkflow_DryRunStillSyncsPerEnvironment(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(workflows.Sync1PasswordEnvWorkflow)
	var a *activities.Activities

	cfg := config.Config{SourceEnv: "dev4", Environments: []string{"dev4"}, ProjectName: "pmn", TLD: "f-ck.xyz"}
	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.FetchSamlCredentials, mock.Anything, mock.Anything).
		Return(activities.FetchSamlCredentialsResult{Credentials: nil}, nil)

	// DryRun still flows through to SyncEnvSecrets - the activity itself
	// decides whether to skip the actual 1Password write (see
	// TestSyncEnvSecrets_DryRunSkipsIngestion), so this mock must still be
	// hit with DryRun: true.
	env.OnActivity(a.SyncEnvSecrets, mock.Anything, mock.MatchedBy(func(in activities.SyncEnvSecretsInput) bool {
		return in.DryRun
	})).Return(activities.SyncEnvSecretsResult{SecretsExtracted: 1}, nil)

	env.ExecuteWorkflow(workflows.Sync1PasswordWorkflow, workflows.Sync1PasswordInput{DryRun: true})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.Sync1PasswordResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.DryRun)
	require.Equal(t, 1, result.SecretsExtracted)
}

// TestSync1PasswordWorkflow_SyncFailureIsNotRetried is the regression test
// for the "op item create isn't idempotent" fix: SyncEnvSecrets (which ends
// with `op item create`) must run with retries disabled, unlike every other
// activity in this workflow, so a failure never risks creating a duplicate
// 1Password item behind the scenes.
func TestSync1PasswordWorkflow_SyncFailureIsNotRetried(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(workflows.Sync1PasswordEnvWorkflow)
	var a *activities.Activities

	cfg := config.Config{SourceEnv: "dev4", Environments: []string{"dev4"}, ProjectName: "pmn", TLD: "f-ck.xyz"}
	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.FetchSamlCredentials, mock.Anything, mock.Anything).
		Return(activities.FetchSamlCredentialsResult{Credentials: nil}, nil)
	env.OnActivity(a.SyncEnvSecrets, mock.Anything, mock.Anything).
		Return(activities.SyncEnvSecretsResult{}, errors.New("op item create: some transient failure"))

	env.ExecuteWorkflow(workflows.Sync1PasswordWorkflow, workflows.Sync1PasswordInput{DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	env.AssertActivityNumberOfCalls(t, "SyncEnvSecrets", 1)
}
