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

func TestGenerateArgocdWorkflow_GeneratesOneManifestPerAppPerEnv(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var a *activities.Activities

	cfg := config.Config{
		ClusterAppsDir: "/cluster-apps",
		ProjectName:    "wtf",
		RepoURL:        "https://github.com/abradner/athena-gitops.git",
		Environments:   []string{"dev4", "dev5"},
	}

	env.OnActivity(a.DiscoverApps, mock.Anything, mock.Anything).
		Return(activities.DiscoverAppsResult{Apps: []string{"wtf-core"}}, nil)

	var written []activities.FileWrite
	env.OnActivity(a.WriteFiles, mock.Anything, mock.MatchedBy(func(in activities.WriteFilesInput) bool {
		written = in.Files
		return true
	})).Return(nil)

	env.ExecuteWorkflow(workflows.GenerateArgocdWorkflow, workflows.GenerateArgocdInput{Config: cfg, DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.GenerateArgocdResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 2, result.ManifestsGenerated)
	require.Len(t, written, 2)

	paths := map[string]bool{}
	for _, f := range written {
		paths[f.Path] = true
		require.Contains(t, f.Content, "kind: Application")
		require.Contains(t, f.Content, "repoURL: https://github.com/abradner/athena-gitops.git")
	}
	require.True(t, paths["/cluster-apps/wtf-core-dev4.yaml"])
	require.True(t, paths["/cluster-apps/wtf-core-dev5.yaml"])
}
