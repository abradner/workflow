// Package embedded runs a workflow against an in-process Temporal dev
// server. It lives in its own package, not in temporalutil itself, so that
// importing the core temporalutil package (as all workflow code does, for
// the activity-call helpers) never links go.temporal.io/server into the
// build: Go's build graph is per-package, and the server dependency is by
// far the heaviest thing in this module. Only binaries that actually offer
// embedded mode - CLI consumers - should import this package.
package embedded

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.temporal.io/server/temporaltest"

	"github.com/abradner/workflow/temporalutil"
)

// Run starts an in-process Temporal dev server (backed by an in-memory
// SQLite persistence layer - the same engine `temporal server start-dev`
// uses), a worker registered with the engine's every workflow and activity,
// executes workflowFn with input, waits for the result, and tears
// everything down. One process, no external dependencies at all - the mode
// for quick local runs.
//
// For anything long-running enough to want durability across process
// restarts, or observability via the Temporal Web UI, use
// temporalutil.RunExternal against a real server instead.
func Run[TIn, TOut any](ctx context.Context, eng temporalutil.Engine, workflowFn func(workflow.Context, TIn) (TOut, error), input TIn) (TOut, error) {
	var zero TOut

	ts := temporaltest.NewServer()
	defer ts.Stop()

	// NewWorker invokes the callback synchronously (and then starts the
	// worker), so regErr is settled before it returns. On error the worker
	// briefly polls with a partial registry until ts.Stop() reaps it -
	// harmless, since nothing else submits to this in-process queue.
	var regErr error
	ts.NewWorker(eng.TaskQueue, func(r worker.Registry) {
		regErr = eng.Register(ctx, r)
	})
	if regErr != nil {
		return zero, fmt.Errorf("registering engine: %w", regErr)
	}

	run, err := ts.GetDefaultClient().ExecuteWorkflow(ctx, temporalutil.StartOptions(eng.TaskQueue), workflowFn, input)
	if err != nil {
		return zero, fmt.Errorf("starting workflow: %w", err)
	}

	var result TOut
	if err := run.Get(ctx, &result); err != nil {
		return zero, err
	}
	return result, nil
}
