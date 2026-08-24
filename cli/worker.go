package cli

import (
	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"

	"github.com/abradner/workflow/temporalutil"
)

// newWorkerCmd is the long-lived worker subcommand every consumer gets.
func newWorkerCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Run a long-lived worker against an external Temporal server",
		Long: "Runs until interrupted, polling Temporal for work. Pair this with --temporal pointing at a real\n" +
			"server (e.g. one started by docker-compose) - the other subcommands become lightweight clients\n" +
			"that just start a workflow and wait for this process to execute it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := opts.Logger()

			target := opts.Temporal
			if target == "" || target == "embedded" {
				target = client.DefaultHostPort
			}

			logger.Info("Worker starting", "taskQueue", opts.app.Engine.TaskQueue, "temporal", target)
			return temporalutil.RunWorker(cmd.Context(), target, log.NewStructuredLogger(logger.Logger), opts.app.Engine)
		},
	}
}
