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

func TestRenderTalosWorkflow_RendersAllTemplatesWhenPlaceholdersResolve(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var a *activities.Activities

	cfg := config.Config{TalosItemID: "item-123", TalosTemplateDir: "/talos"}

	env.OnActivity(a.ReadOnePasswordNote, mock.Anything, activities.ReadOnePasswordNoteInput{ItemID: "item-123"}).
		Return(activities.ReadOnePasswordNoteResult{Content: "cluster:\n  id: abc123\n"}, nil)

	env.OnActivity(a.ReadTemplateFiles, mock.Anything, activities.ReadTemplateFilesInput{TemplateDir: "/talos"}).
		Return(activities.ReadTemplateFilesResult{
			Paths:    []string{"/talos/config.template.yaml"},
			Contents: []string{"id: {{ cluster.id }}\n"},
		}, nil)

	var written []activities.FileWrite
	env.OnActivity(a.WriteFiles, mock.Anything, mock.MatchedBy(func(in activities.WriteFilesInput) bool {
		written = in.Files
		return true
	})).Return(nil)

	env.ExecuteWorkflow(workflows.RenderTalosWorkflow, workflows.RenderTalosInput{Config: cfg, DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.RenderTalosResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 1, result.TemplatesRendered)

	require.Len(t, written, 1)
	require.Equal(t, "/talos/config.yaml", written[0].Path)
	require.Equal(t, "id: abc123\n", written[0].Content)
}

func TestRenderTalosWorkflow_FailsWithUnresolvedPlaceholders(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var a *activities.Activities

	cfg := config.Config{TalosItemID: "item-123", TalosTemplateDir: "/talos"}

	env.OnActivity(a.ReadOnePasswordNote, mock.Anything, mock.Anything).
		Return(activities.ReadOnePasswordNoteResult{Content: "cluster:\n  id: abc123\n"}, nil)
	env.OnActivity(a.ReadTemplateFiles, mock.Anything, mock.Anything).
		Return(activities.ReadTemplateFilesResult{
			Paths:    []string{"/talos/config.template.yaml"},
			Contents: []string{"id: {{ cluster.id }}\ntoken: {{ missing.token }}\n"},
		}, nil)
	// No WriteFiles mock - missing placeholders must fail before commit.

	env.ExecuteWorkflow(workflows.RenderTalosWorkflow, workflows.RenderTalosInput{Config: cfg, DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.ErrorContains(t, err, "unresolved placeholder")
}
