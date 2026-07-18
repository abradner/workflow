package workflows

import (
	"fmt"

	"go.temporal.io/sdk/workflow"

	"github.com/abradner/workflow/internal/activities"
)

// RenderTalosInput is the `render-talos` command's workflow input.
type RenderTalosInput struct {
	DryRun bool
}

// RenderTalosResult summarizes what the workflow did.
type RenderTalosResult struct {
	SecretKeysLoaded  int
	TemplatesRendered int
	DryRun            bool
}

// RenderTalosWorkflow reads a 1Password Secure Note containing the Talos
// secrets.yaml content, then hydrates every *.template.yaml file in
// Config.TalosTemplateDir by substituting "{{ dotted.key }}" placeholders.
// Unlike the other workflows, this one never touches app discovery or the
// transformer pipeline - it delegates its entire read/render/write flow to
// one activity, RenderTalosTemplates.
//
// That's a deliberate difference from how earlier versions of this workflow
// worked: reading the Secure Note and rendering templates used to happen
// here, in workflow code, with the Secure Note's content and the rendered
// (secret-bearing) files each crossing an activity boundary of their own.
// Both are real cluster secrets, and Temporal records every activity result
// and input in its durable event history - visible via the Web UI/API/DB in
// external mode - so neither can be allowed to cross back into workflow
// code at all. See the doc comment on activities.RenderTalosTemplates.
func RenderTalosWorkflow(ctx workflow.Context, in RenderTalosInput) (RenderTalosResult, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	logger := workflow.GetLogger(ctx)
	var a *activities.Activities

	// Config is loaded on whichever machine runs the worker, not wherever
	// this workflow was started from - see the doc comment on LoadConfig.
	cfgResult, err := runActivity[activities.LoadConfigResult](ctx, a.LoadConfig)
	if err != nil {
		return RenderTalosResult{}, fmt.Errorf("loading config: %w", err)
	}
	cfg := cfgResult.Config

	logger.Info("Rendering Talos templates", "itemID", cfg.TalosItemID, "dir", cfg.TalosTemplateDir)
	rendered, err := runActivity[activities.RenderTalosTemplatesResult](ctx, a.RenderTalosTemplates, activities.RenderTalosTemplatesInput{
		ItemID:      cfg.TalosItemID,
		TemplateDir: cfg.TalosTemplateDir,
		DryRun:      in.DryRun,
	})
	if err != nil {
		return RenderTalosResult{}, fmt.Errorf("rendering Talos templates: %w", err)
	}

	if in.DryRun {
		logger.Info("Dry run: no files written", "wouldWriteFiles", rendered.TemplatesRendered)
	} else {
		logger.Info("Wrote rendered templates", "count", rendered.TemplatesRendered)
	}

	return RenderTalosResult{
		SecretKeysLoaded:  rendered.SecretKeysLoaded,
		TemplatesRendered: rendered.TemplatesRendered,
		DryRun:            in.DryRun,
	}, nil
}
