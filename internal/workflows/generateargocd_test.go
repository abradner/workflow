package workflows_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"gopkg.in/yaml.v3"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/workflows"
)

// TestGenerateArgocdWorkflow_GeneratesApplicationSetForEveryAppAndEnv is the
// regression test for switching from one Application manifest file per
// app x env pair to a single ApplicationSet with a matrix generator - see
// the doc comment on GenerateArgocdWorkflow for why: writing individual
// per-app-per-env files would fight the target GitOps repo's
// ApplicationSet for ownership of the same Application names.
func TestGenerateArgocdWorkflow_GeneratesApplicationSetForEveryAppAndEnv(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var a *activities.Activities

	cfg := config.Config{
		ClusterAppsDir: "/cluster-apps",
		ProjectName:    "wtf",
		RepoURL:        "https://github.com/abradner/wtf-workloads.git",
		Environments:   []string{"dev4", "dev5"},
	}

	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.DiscoverApps, mock.Anything, mock.Anything).
		Return(activities.DiscoverAppsResult{Apps: []string{"wtf-core", "wtf-ui"}}, nil)

	var written []activities.FileWrite
	env.OnActivity(a.WriteFiles, mock.Anything, mock.MatchedBy(func(in activities.WriteFilesInput) bool {
		written = in.Files
		return true
	})).Return(nil)

	env.ExecuteWorkflow(workflows.GenerateArgocdWorkflow, workflows.GenerateArgocdInput{DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.GenerateArgocdResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 2, result.AppsGenerated)
	require.Equal(t, 2, result.EnvsGenerated)
	require.False(t, result.DryRun)

	require.Len(t, written, 1, "everything lives in one ApplicationSet file, not one file per app/env")
	require.Equal(t, "/cluster-apps/wtf-appset.yaml", written[0].Path)

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(written[0].Content), &doc))
	require.Equal(t, "ApplicationSet", doc["kind"])
	require.Equal(t, "wtf", doc["metadata"].(map[string]any)["name"])

	spec := doc["spec"].(map[string]any)
	generators := spec["generators"].([]any)
	require.Len(t, generators, 1)
	matrixGenerators := generators[0].(map[string]any)["matrix"].(map[string]any)["generators"].([]any)
	require.Len(t, matrixGenerators, 2, "one list generator for envs, one for apps")

	envElements := matrixGenerators[0].(map[string]any)["list"].(map[string]any)["elements"]
	require.Equal(t, []any{
		map[string]any{"env": "dev4"},
		map[string]any{"env": "dev5"},
	}, envElements)

	appElements := matrixGenerators[1].(map[string]any)["list"].(map[string]any)["elements"]
	require.Equal(t, []any{
		map[string]any{"app": "wtf-core"},
		map[string]any{"app": "wtf-ui"},
	}, appElements)

	template := spec["template"].(map[string]any)
	require.Equal(t, "{{.app}}-{{.env}}", template["metadata"].(map[string]any)["name"])

	templateSpec := template["spec"].(map[string]any)
	source := templateSpec["source"].(map[string]any)
	require.Equal(t, "https://github.com/abradner/wtf-workloads.git", source["repoURL"])
	require.Equal(t, "{{.app}}/overlay/{{.env}}", source["path"])
	require.Equal(t, "wtf-{{.env}}", templateSpec["destination"].(map[string]any)["namespace"])
}

func TestGenerateArgocdWorkflow_DryRunSkipsWrite(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var a *activities.Activities

	cfg := config.Config{
		ClusterAppsDir: "/cluster-apps",
		ProjectName:    "wtf",
		RepoURL:        "https://github.com/abradner/wtf-workloads.git",
		Environments:   []string{"dev4"},
	}

	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.DiscoverApps, mock.Anything, mock.Anything).
		Return(activities.DiscoverAppsResult{Apps: []string{"wtf-core"}}, nil)
	// Deliberately no WriteFiles mock: DryRun must not call it.

	env.ExecuteWorkflow(workflows.GenerateArgocdWorkflow, workflows.GenerateArgocdInput{DryRun: true})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.GenerateArgocdResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.DryRun)
	require.Equal(t, 1, result.AppsGenerated)
	require.Equal(t, 1, result.EnvsGenerated)
}
