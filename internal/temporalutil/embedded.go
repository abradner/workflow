package temporalutil

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.temporal.io/server/temporaltest"

	"github.com/abradner/workflow/internal/activities"
)

// RunEmbedded starts an in-process Temporal dev server (backed by an
// in-memory SQLite persistence layer - the same engine `temporal server
// start-dev` uses), a worker registered with every workflow and activity,
// executes workflowFn with input, waits for the result, and tears
// everything down. One process, no external dependencies at all - the mode
// for quick local runs.
//
// For anything long-running enough to want durability across process
// restarts, or observability via the Temporal Web UI, use RunExternal
// against a real server instead (see docker-compose.yml).
func RunEmbedded[TIn, TOut any](ctx context.Context, workflowFn func(workflow.Context, TIn) (TOut, error), input TIn) (TOut, error) {
	var zero TOut

	acts, err := activities.New(ctx)
	if err != nil {
		return zero, fmt.Errorf("building activities: %w", err)
	}

	ts := temporaltest.NewServer()
	defer ts.Stop()

	ts.NewWorker(TaskQueue, func(r worker.Registry) {
		RegisterAll(r, acts)
	})

	run, err := ts.GetDefaultClient().ExecuteWorkflow(ctx, client.StartWorkflowOptions{TaskQueue: TaskQueue}, workflowFn, input)
	if err != nil {
		return zero, fmt.Errorf("starting workflow: %w", err)
	}

	var result TOut
	if err := run.Get(ctx, &result); err != nil {
		return zero, err
	}
	return result, nil
}
