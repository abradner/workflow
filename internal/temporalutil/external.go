package temporalutil

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/workflow"
)

// RunExternal dials an existing Temporal server at hostPort - typically the
// one started by docker-compose - and executes workflowFn with input,
// waiting for its result. It does not start a worker itself: a separate
// long-lived `workflow worker` process (see cmd/workflow) must already be
// polling TaskQueue.
func RunExternal[TIn, TOut any](ctx context.Context, hostPort string, logger log.Logger, workflowFn func(workflow.Context, TIn) (TOut, error), input TIn) (TOut, error) {
	var zero TOut

	c, err := client.Dial(client.Options{HostPort: hostPort, Logger: logger})
	if err != nil {
		return zero, fmt.Errorf("dialing temporal at %s: %w", hostPort, err)
	}
	defer c.Close()

	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{TaskQueue: TaskQueue}, workflowFn, input)
	if err != nil {
		return zero, fmt.Errorf("starting workflow: %w", err)
	}

	var result TOut
	if err := run.Get(ctx, &result); err != nil {
		return zero, err
	}
	return result, nil
}
