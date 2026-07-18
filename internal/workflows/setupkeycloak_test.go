package workflows_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/workflows"
)

func TestSetupKeycloakWorkflow_DryRunDoesNothing(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(workflows.SetupKeycloakEnvWorkflow)
	var a *activities.Activities

	cfg := config.Config{Environments: []string{"dev4"}, ProjectName: "pmn", TLD: "f-ck.xyz", DestDir: "/dest"}
	mockLoadConfig(env, a, cfg)

	// No other activities mocked: SetupKeycloak's provisioning work all
	// lives in the (Ruby-called) commit phase, which dry-run skips
	// entirely - any activity call here (besides LoadConfig, which always
	// runs) would fail the test.
	env.ExecuteWorkflow(workflows.SetupKeycloakWorkflow, workflows.SetupKeycloakInput{DryRun: true})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.SetupKeycloakResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.DryRun)
	require.Equal(t, 0, result.EnvironmentsSucceeded)
}

func TestSetupKeycloakWorkflow_ProvisionsAReadyEnvironment(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(workflows.SetupKeycloakEnvWorkflow)
	var a *activities.Activities

	cfg := config.Config{
		Environments: []string{"dev4"},
		ProjectName:  "pmn",
		TLD:          "f-ck.xyz",
		DestDir:      "/dest",
	}
	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.CheckKeycloakReady, mock.Anything, mock.Anything).
		Return(activities.CheckKeycloakReadyResult{Ready: true}, nil)
	// RunKeycloakSetup loads the admin credentials itself (see its doc
	// comment) rather than receiving them as input, so the mock only needs
	// to match on BaseURL.
	env.OnActivity(a.RunKeycloakSetup, mock.Anything, activities.RunKeycloakSetupInput{
		BaseURL: "https://pmn-keycloak.pmn.dev4.f-ck.xyz",
	}).Return(activities.RunKeycloakSetupResult{XML: "<xml/>", B64: "PHhtbC8+"}, nil)

	var written []activities.FileWrite
	env.OnActivity(a.WriteFiles, mock.Anything, mock.MatchedBy(func(in activities.WriteFilesInput) bool {
		written = in.Files
		return true
	})).Return(nil)

	env.ExecuteWorkflow(workflows.SetupKeycloakWorkflow, workflows.SetupKeycloakInput{DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.SetupKeycloakResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 1, result.EnvironmentsAttempted)
	require.Equal(t, 1, result.EnvironmentsSucceeded)

	require.Len(t, written, 2)
	paths := map[string]string{written[0].Path: written[0].Content, written[1].Path: written[1].Content}
	require.Equal(t, "<xml/>", paths["/dest/pmn-keycloak/overlay/dev4/sso.xml"])
	require.Equal(t, "PHhtbC8+", paths["/dest/pmn-keycloak/overlay/dev4/sso.xml.b64"])
}

func TestSetupKeycloakWorkflow_OneEnvironmentFailingDoesNotStopTheOthers(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(workflows.SetupKeycloakEnvWorkflow)
	var a *activities.Activities

	cfg := config.Config{Environments: []string{"dev-broken", "dev-ok"}, ProjectName: "pmn", TLD: "f-ck.xyz", DestDir: "/dest"}
	mockLoadConfig(env, a, cfg)

	// dev-broken never becomes ready - the workflow gives up after
	// keycloakReadyMaxAttempts polls (durable-timer waits between them,
	// which the test environment fast-forwards through).
	env.OnActivity(a.CheckKeycloakReady, mock.Anything, mock.MatchedBy(func(in activities.CheckKeycloakReadyInput) bool {
		return in.BaseURL == "https://pmn-keycloak.pmn.dev-broken.f-ck.xyz"
	})).Return(activities.CheckKeycloakReadyResult{Ready: false}, nil)

	env.OnActivity(a.CheckKeycloakReady, mock.Anything, mock.MatchedBy(func(in activities.CheckKeycloakReadyInput) bool {
		return in.BaseURL == "https://pmn-keycloak.pmn.dev-ok.f-ck.xyz"
	})).Return(activities.CheckKeycloakReadyResult{Ready: true}, nil)

	env.OnActivity(a.RunKeycloakSetup, mock.Anything, mock.Anything).
		Return(activities.RunKeycloakSetupResult{XML: "<xml/>", B64: "PHhtbC8+"}, nil)
	env.OnActivity(a.WriteFiles, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(workflows.SetupKeycloakWorkflow, workflows.SetupKeycloakInput{DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.SetupKeycloakResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 2, result.EnvironmentsAttempted)
	require.Equal(t, 1, result.EnvironmentsSucceeded, "dev-ok should succeed even though dev-broken never became ready")
}
