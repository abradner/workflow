package main

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/workflow"

	"github.com/abradner/workflow/temporalutil"
	"github.com/abradner/workflow/temporalutil/embedded"
)

// runWorkflow runs a workflow (embedded or external per opts.Temporal) and
// reports the result. Generic over each workflow's distinct input/output
// types so every subcommand in commands.go is a one-line wrapper around
// this.
//
// Notably, this does NOT load Config - each workflow does that itself as
// its first step, via the LoadConfig activity, so Config always reflects
// wherever the worker actually runs. In external mode that's a different
// process (and potentially a different machine/container/filesystem
// entirely) from wherever this CLI command is invoked - see the doc
// comment on activities.Activities.LoadConfig.
func runWorkflow[TIn, TOut any](
	ctx context.Context,
	opts *globalOptions,
	workflowFn func(workflow.Context, TIn) (TOut, error),
	input TIn,
) error {
	logger := opts.logger()

	logger.Section("Starting Workflow")
	if opts.DryRun {
		logger.Info("[DRY RUN] planning only - no state changes will be made")
	}

	var result TOut
	var err error
	if opts.Temporal == "" || opts.Temporal == "embedded" {
		logger.Info("Running against an embedded, in-process Temporal server")
		result, err = embedded.Run(ctx, engine, workflowFn, input)
	} else {
		logger.Info("Running against external Temporal server", "target", opts.Temporal)
		result, err = temporalutil.RunExternal(ctx, opts.Temporal, log.NewStructuredLogger(logger.Logger), engine, workflowFn, input)
	}
	if err != nil {
		return err
	}

	logger.Info("Workflow complete", "result", fmt.Sprintf("%+v", result))
	return nil
}
