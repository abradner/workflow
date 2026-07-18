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

	env.OnActivity(a.BuildAppFiles, mock.Anything, mock.MatchedBy(func(in activities.BuildAppFilesInput) bool {
		return in.AppName == "app1"
	})).Return(activities.BuildAppFilesResult{Files: []activities.FileWrite{{Path: "/dest/app1/base/x.yaml", Content: "a"}}}, nil)

	env.OnActivity(a.BuildAppFiles, mock.Anything, mock.MatchedBy(func(in activities.BuildAppFilesInput) bool {
		return in.AppName == "app2"
	})).Return(activities.BuildAppFilesResult{Files: []activities.FileWrite{{Path: "/dest/app2/base/y.yaml", Content: "b"}}}, nil)

	// Each app now commits via its own child workflow's own WriteFiles call
	// (one call per app) rather than one aggregate call at the end, so this
	// mock fires twice - accumulate across both instead of assuming one.
	var writtenFiles []activities.FileWrite
	env.OnActivity(a.WriteFiles, mock.Anything, mock.MatchedBy(func(in activities.WriteFilesInput) bool {
		writtenFiles = append(writtenFiles, in.Files...)
		return true
	})).Return(nil)

	env.ExecuteWorkflow(workflows.SyncWorkloadsWorkflow, workflows.SyncWorkloadsInput{DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.SyncWorkloadsResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 2, result.AppsProcessed)
	require.Equal(t, 2, result.FilesWritten)
	require.False(t, result.DryRun)
	require.Len(t, writtenFiles, 2)
}

func TestSyncWorkloadsWorkflow_DryRunSkipsWriteFiles(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(workflows.SyncAppWorkflow)
	var a *activities.Activities

	cfg := config.Config{SourceDir: "/src", DestDir: "/dest", SourceEnv: "dev3", Environments: []string{"dev4"}}
	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.DiscoverApps, mock.Anything, mock.Anything).
		Return(activities.DiscoverAppsResult{Apps: []string{"app1"}}, nil)
	env.OnActivity(a.BuildAppFiles, mock.Anything, mock.Anything).
		Return(activities.BuildAppFilesResult{Files: []activities.FileWrite{{Path: "/dest/app1/base/x.yaml", Content: "a"}}}, nil)
	// Deliberately no WriteFiles mock: if the workflow tried to call it under
	// DryRun, the test environment would fail with "no mock for WriteFiles".

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

	env.OnActivity(a.BuildAppFiles, mock.Anything, mock.MatchedBy(func(in activities.BuildAppFilesInput) bool {
		return in.AppName == "app-ok"
	})).Return(activities.BuildAppFilesResult{Files: []activities.FileWrite{{Path: "/dest/app-ok/base/x.yaml", Content: "a"}}}, nil)

	env.OnActivity(a.WriteFiles, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(workflows.SyncWorkloadsWorkflow, workflows.SyncWorkloadsInput{DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.ErrorContains(t, err, "app-broken")
	env.AssertActivityNumberOfCalls(t, "WriteFiles", 1)
}
