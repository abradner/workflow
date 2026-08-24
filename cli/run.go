package cli

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/workflow"

	"github.com/abradner/workflow/temporalutil"
	"github.com/abradner/workflow/temporalutil/embedded"
)

// Run runs a workflow (embedded or external per opts.Temporal) and reports
// the result. Generic over each workflow's distinct input/output types so
// every subcommand is a one-line wrapper around this.
//
// Notably, this does NOT load the consumer's Config - each workflow should
// do that itself as its first step, via a LoadConfig activity, so Config
// always reflects wherever the worker actually runs. In external mode
// that's a different process (and potentially a different
// machine/container/filesystem entirely) from wherever this CLI command is
// invoked.
//
// The workflow result is logged to stdout in full (%+v). Keep Result
// structs to counts, names and bounded summaries - never secret material,
// never unbounded content. The same rule already governs what may cross a
// workflow/activity boundary (Temporal records it all in durable plaintext
// event history); this logging is one more reason it must hold.
func Run[TIn, TOut any](
	ctx context.Context,
	opts *Options,
	workflowFn func(workflow.Context, TIn) (TOut, error),
	input TIn,
) error {
	logger := opts.Logger()

	logger.Section("Starting Workflow")
	if opts.DryRun {
		logger.Info("[DRY RUN] planning only - no state changes will be made")
	}

	var result TOut
	var err error
	if opts.Temporal == "" || opts.Temporal == "embedded" {
		logger.Info("Running against an embedded, in-process Temporal server")
		result, err = embedded.Run(ctx, opts.app.Engine, workflowFn, input)
	} else {
		logger.Info("Running against external Temporal server", "target", opts.Temporal)
		result, err = temporalutil.RunExternal(ctx, opts.Temporal, log.NewStructuredLogger(logger.Logger), opts.app.Engine, workflowFn, input)
	}
	if err != nil {
		return err
	}

	logger.Info("Workflow complete", "result", fmt.Sprintf("%+v", result))
	return nil
}
