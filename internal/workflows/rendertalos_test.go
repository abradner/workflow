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

// RenderTalosWorkflow now delegates its entire read/render/write flow to
// one activity (RenderTalosTemplates), specifically so the Secure Note
// content and rendered files never cross back into workflow code - see its
// doc comment in internal/activities/activities.go. That means these tests
// only assert on wiring (correct ItemID/TemplateDir/DryRun in, correct
// counts out); the actual rendering behavior has its own coverage in
// internal/activities/activities_test.go.

func TestRenderTalosWorkflow_PassesConfigThroughAndReportsCounts(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var a *activities.Activities

	cfg := config.Config{TalosItemID: "item-123", TalosTemplateDir: "/talos"}
	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.RenderTalosTemplates, mock.Anything, activities.RenderTalosTemplatesInput{
		ItemID:      "item-123",
		TemplateDir: "/talos",
		DryRun:      false,
	}).Return(activities.RenderTalosTemplatesResult{SecretKeysLoaded: 3, TemplatesRendered: 2}, nil)

	env.ExecuteWorkflow(workflows.RenderTalosWorkflow, workflows.RenderTalosInput{DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.RenderTalosResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 3, result.SecretKeysLoaded)
	require.Equal(t, 2, result.TemplatesRendered)
	require.False(t, result.DryRun)
}

func TestRenderTalosWorkflow_PassesDryRunThrough(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var a *activities.Activities

	cfg := config.Config{TalosItemID: "item-123", TalosTemplateDir: "/talos"}
	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.RenderTalosTemplates, mock.Anything, mock.MatchedBy(func(in activities.RenderTalosTemplatesInput) bool {
		return in.DryRun
	})).Return(activities.RenderTalosTemplatesResult{SecretKeysLoaded: 1, TemplatesRendered: 1}, nil)

	env.ExecuteWorkflow(workflows.RenderTalosWorkflow, workflows.RenderTalosInput{DryRun: true})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.RenderTalosResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.DryRun)
}

func TestRenderTalosWorkflow_PropagatesActivityError(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var a *activities.Activities

	cfg := config.Config{TalosItemID: "item-123", TalosTemplateDir: "/talos"}
	mockLoadConfig(env, a, cfg)

	env.OnActivity(a.RenderTalosTemplates, mock.Anything, mock.Anything).
		Return(activities.RenderTalosTemplatesResult{}, errors.New("cannot hydrate: 1 unresolved placeholder(s)"))

	env.ExecuteWorkflow(workflows.RenderTalosWorkflow, workflows.RenderTalosInput{DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.ErrorContains(t, err, "unresolved placeholder")
}
