package main

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/workflow"

	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/temporalutil"
)

// runWorkflow loads Config, builds workflowFn's input from it, runs the
// workflow (embedded or external per opts.Temporal), and reports the result.
// Generic over each workflow's distinct input/output types so every
// subcommand in commands.go is a two-line wrapper around this.
func runWorkflow[TIn, TOut any](
	ctx context.Context,
	opts *globalOptions,
	workflowFn func(workflow.Context, TIn) (TOut, error),
	buildInput func(config.Config) TIn,
) error {
	logger := opts.logger()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load configuration", err)
	}

	logger.Section("Starting Workflow")
	if opts.DryRun {
		logger.Info("[DRY RUN] planning only - no state changes will be made")
	}

	input := buildInput(*cfg)

	var result TOut
	if opts.Temporal == "" || opts.Temporal == "embedded" {
		logger.Info("Running against an embedded, in-process Temporal server")
		result, err = temporalutil.RunEmbedded(ctx, workflowFn, input)
	} else {
		logger.Info("Running against external Temporal server", "target", opts.Temporal)
		result, err = temporalutil.RunExternal(ctx, opts.Temporal, log.NewStructuredLogger(logger.Logger), workflowFn, input)
	}
	if err != nil {
		return err
	}

	logger.Info("Workflow complete", "result", fmt.Sprintf("%+v", result))
	return nil
}
