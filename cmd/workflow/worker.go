package main

import (
	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/temporalutil"
)

func newWorkerCmd(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Run a long-lived worker against an external Temporal server",
		Long: "Runs until interrupted, polling Temporal for work. Pair this with --temporal pointing at a real\n" +
			"server (e.g. the one docker-compose.yml starts) - the other subcommands become lightweight clients\n" +
			"that just start a workflow and wait for this process to execute it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := opts.logger()
			ctx := cmd.Context()

			target := opts.Temporal
			if target == "" || target == "embedded" {
				target = client.DefaultHostPort
			}

			c, err := client.Dial(client.Options{HostPort: target, Logger: log.NewStructuredLogger(logger.Logger)})
			if err != nil {
				return err
			}
			defer c.Close()

			acts, err := activities.New(ctx)
			if err != nil {
				return err
			}

			w := worker.New(c, temporalutil.TaskQueue, worker.Options{})
			temporalutil.RegisterAll(w, acts)

			logger.Info("Worker started", "taskQueue", temporalutil.TaskQueue, "temporal", target)
			return w.Run(worker.InterruptCh())
		},
	}
}
