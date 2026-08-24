package temporalutil

import (
	"context"
	"errors"
	"fmt"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// RunExternal dials an existing Temporal server at hostPort - typically one
// started by docker-compose - and executes workflowFn with input, waiting
// for its result. It does not start a worker itself: a separate long-lived
// worker process (see RunWorker) must already be polling eng.TaskQueue.
func RunExternal[TIn, TOut any](ctx context.Context, hostPort string, logger log.Logger, eng Engine, workflowFn func(workflow.Context, TIn) (TOut, error), input TIn) (TOut, error) {
	var zero TOut

	if eng.TaskQueue == "" {
		return zero, errors.New("temporalutil: Engine.TaskQueue must not be empty")
	}

	c, err := client.Dial(client.Options{HostPort: hostPort, Logger: logger})
	if err != nil {
		return zero, fmt.Errorf("dialing temporal at %s: %w", hostPort, err)
	}
	defer c.Close()

	run, err := c.ExecuteWorkflow(ctx, StartOptions(eng.TaskQueue), workflowFn, input)
	if err != nil {
		return zero, fmt.Errorf("starting workflow: %w", err)
	}

	var result TOut
	if err := run.Get(ctx, &result); err != nil {
		return zero, err
	}
	return result, nil
}

// RunWorker dials the Temporal server at hostPort and runs a long-lived
// worker registered with every workflow and activity the engine defines. Pair
// with RunExternal: the CLI commands become lightweight clients that just
// start a workflow and wait for this process to execute it.
//
// ctx is used for registration (building the engine's dependencies), NOT
// for lifetime control: the worker polls until the process is interrupted
// (worker.InterruptCh), matching its role as a foreground subcommand.
func RunWorker(ctx context.Context, hostPort string, logger log.Logger, eng Engine) error {
	if err := eng.Validate(); err != nil {
		return err
	}

	c, err := client.Dial(client.Options{HostPort: hostPort, Logger: logger})
	if err != nil {
		return fmt.Errorf("dialing temporal at %s: %w", hostPort, err)
	}
	defer c.Close()

	w := worker.New(c, eng.TaskQueue, worker.Options{})
	if err := eng.Register(ctx, w); err != nil {
		return fmt.Errorf("registering engine: %w", err)
	}

	return w.Run(worker.InterruptCh())
}
