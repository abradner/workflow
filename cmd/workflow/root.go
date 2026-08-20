package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/abradner/workflow/logging"
)

// globalOptions holds the flags every subcommand shares.
type globalOptions struct {
	DryRun   bool
	Verbose  bool
	Temporal string
}

func (o *globalOptions) logger() *logging.Logger {
	level := slog.LevelInfo
	if o.Verbose {
		level = slog.LevelDebug
	}
	return logging.New(os.Stdout, level)
}

func newRootCmd() *cobra.Command {
	opts := &globalOptions{}

	root := &cobra.Command{
		Use:           "workflow",
		Short:         "A Go + Temporal ETL pipeline for migrating, transforming and adapting Kubernetes workloads",
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.PersistentFlags().BoolVar(&opts.DryRun, "dry-run", false, "Run without making state changes")
	root.PersistentFlags().BoolVarP(&opts.Verbose, "verbose", "v", false, "Enable debug logging")
	root.PersistentFlags().StringVar(&opts.Temporal, "temporal", "embedded",
		`"embedded" runs an in-process Temporal dev server for just this command (default, zero dependencies); `+
			`a host:port (e.g. localhost:7233) dials an existing Temporal server instead - see docker-compose.yml `+
			`and the "worker" subcommand, which must already be running against that server`)

	root.AddCommand(
		newSyncCmd(opts),
		newSetupArgoCmd(opts),
		newSync1PasswordCmd(opts),
		newPrune1PasswordCmd(opts),
		newRenderTalosCmd(opts),
		newSetupKeycloakCmd(opts),
		newWorkerCmd(opts),
	)

	return root
}
