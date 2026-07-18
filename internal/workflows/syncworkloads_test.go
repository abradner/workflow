package workflows_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/workflows"
)

func TestSyncWorkloadsWorkflow_CommitsFilesBuiltForEveryApp(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(workflows.SyncAppWorkflow)
	var a *activities.Activities

	cfg := config.Config{SourceDir: "/src", DestDir: "/dest", SourceEnv: "dev3", Environments: []string{"dev4"}}
	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.DiscoverApps, mock.Anything, mock.Anything).
		Return(activities.DiscoverAppsResult{Apps: []string{"app1", "app2"}}, nil)

	// BuildAppFiles now writes internally (see its doc comment) rather than
	// returning content for a separate WriteFiles call, so mocking it is
	// enough to exercise the whole per-app commit.
	env.OnActivity(a.BuildAppFiles, mock.Anything, mock.MatchedBy(func(in activities.BuildAppFilesInput) bool {
		return in.AppName == "app1" && !in.DryRun
	})).Return(activities.BuildAppFilesResult{FilesWritten: 1}, nil)

	env.OnActivity(a.BuildAppFiles, mock.Anything, mock.MatchedBy(func(in activities.BuildAppFilesInput) bool {
		return in.AppName == "app2" && !in.DryRun
	})).Return(activities.BuildAppFilesResult{FilesWritten: 1}, nil)

	env.ExecuteWorkflow(workflows.SyncWorkloadsWorkflow, workflows.SyncWorkloadsInput{DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.SyncWorkloadsResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 2, result.AppsProcessed)
	require.Equal(t, 2, result.FilesWritten)
	require.False(t, result.DryRun)
}

func TestSyncWorkloadsWorkflow_DryRunSkipsWriting(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(workflows.SyncAppWorkflow)
	var a *activities.Activities

	cfg := config.Config{SourceDir: "/src", DestDir: "/dest", SourceEnv: "dev3", Environments: []string{"dev4"}}
	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.DiscoverApps, mock.Anything, mock.Anything).
		Return(activities.DiscoverAppsResult{Apps: []string{"app1"}}, nil)
	// DryRun must still flow through to BuildAppFiles - it decides
	// internally whether to actually write (see
	// TestBuildAppFiles_DryRunWritesNothing at the activity level).
	env.OnActivity(a.BuildAppFiles, mock.Anything, mock.MatchedBy(func(in activities.BuildAppFilesInput) bool {
		return in.DryRun
	})).Return(activities.BuildAppFilesResult{FilesWritten: 1}, nil)

	env.ExecuteWorkflow(workflows.SyncWorkloadsWorkflow, workflows.SyncWorkloadsInput{DryRun: true})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.SyncWorkloadsResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.DryRun)
	require.Equal(t, 1, result.FilesWritten, "the plan is still reported even though nothing was written")
}

// TestSyncWorkloadsWorkflow_OneAppFailingStillProcessesTheOthers is the
// regression test for fanning out per-app child workflows: a failure in one
// app's child must not stop the others from building and committing - the
// parent waits for every child before deciding the overall outcome, and
// reports every failure rather than just the first one.
func TestSyncWorkloadsWorkflow_OneAppFailingStillProcessesTheOthers(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(workflows.SyncAppWorkflow)
	var a *activities.Activities

	cfg := config.Config{SourceDir: "/src", DestDir: "/dest", SourceEnv: "dev3", Environments: []string{"dev4"}}
	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.DiscoverApps, mock.Anything, mock.Anything).
		Return(activities.DiscoverAppsResult{Apps: []string{"app-broken", "app-ok"}}, nil)

	env.OnActivity(a.BuildAppFiles, mock.Anything, mock.MatchedBy(func(in activities.BuildAppFilesInput) bool {
		return in.AppName == "app-broken"
	})).Return(activities.BuildAppFilesResult{}, errors.New("boom"))

	appOkBuilt := false
	env.OnActivity(a.BuildAppFiles, mock.Anything, mock.MatchedBy(func(in activities.BuildAppFilesInput) bool {
		if in.AppName != "app-ok" {
			return false
		}
		appOkBuilt = true
		return true
	})).Return(activities.BuildAppFilesResult{FilesWritten: 1}, nil)

	env.ExecuteWorkflow(workflows.SyncWorkloadsWorkflow, workflows.SyncWorkloadsInput{DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.ErrorContains(t, err, "app-broken")
	require.True(t, appOkBuilt, "app-ok's build must still run even though app-broken failed")
}
