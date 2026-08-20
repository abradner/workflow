// Package cli is the platform's command-line harness. A consumer describes
// itself once - name, blurb, Engine - and gets the full CLI contract for
// free: the global --dry-run/--verbose/--temporal flags, a `worker`
// subcommand for external mode, and Run, the generic
// start-a-workflow-and-wait wrapper every subcommand is a one-liner around.
//
// The intended consumer main.go is ~15 lines:
//
//	app := cli.App{Name: "mytool", Short: "...", Engine: myEngine}
//	root := cli.New(app, newFooCmd, newBarCmd)
//	if err := root.Execute(); err != nil { os.Exit(1) }
//
// where each command factory is:
//
//	func newFooCmd(opts *cli.Options) *cobra.Command {
//		return &cobra.Command{Use: "foo", RunE: func(cmd *cobra.Command, _ []string) error {
//			return cli.Run(cmd.Context(), opts, workflows.FooWorkflow, workflows.FooInput{DryRun: opts.DryRun})
//		}}
//	}
package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/abradner/workflow/logging"
	"github.com/abradner/workflow/temporalutil"
)

// App identifies one consumer tool: its command name, its one-line
// description, and its Temporal surface.
type App struct {
	Name   string
	Short  string
	Engine temporalutil.Engine
}

// Options holds the flags every subcommand shares, plus the App they belong
// to. Command factories receive one *Options and may read the flag values
// at RunE time (cobra has bound them by then). Only New produces a usable
// Options - the zero value has no App and will panic inside Run.
type Options struct {
	DryRun   bool
	Verbose  bool
	Temporal string

	app App
}

// Logger builds the console logger honoring --verbose.
func (o *Options) Logger() *logging.Logger {
	level := slog.LevelInfo
	if o.Verbose {
		level = slog.LevelDebug
	}
	return logging.New(os.Stdout, level)
}

// New assembles the root command: global flags, the worker subcommand, and
// one subcommand per factory. Factories run immediately (to build their
// cobra.Command) but must only read Options values inside RunE.
//
// The worker subcommand is always added by the platform - do not pass a
// factory whose command is also named "worker"; cobra resolves duplicate
// names silently rather than erroring.
func New(app App, commands ...func(*Options) *cobra.Command) *cobra.Command {
	opts := &Options{app: app}

	root := &cobra.Command{
		Use:           app.Name,
		Short:         app.Short,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.PersistentFlags().BoolVar(&opts.DryRun, "dry-run", false, "Run without making state changes")
	root.PersistentFlags().BoolVarP(&opts.Verbose, "verbose", "v", false, "Enable debug logging")
	root.PersistentFlags().StringVar(&opts.Temporal, "temporal", "embedded",
		`"embedded" runs an in-process Temporal dev server for just this command (default, zero dependencies); `+
			`a host:port (e.g. localhost:7233) dials an existing Temporal server instead - see the "worker" `+
			`subcommand, which must already be running against that server`)

	for _, newCmd := range commands {
		root.AddCommand(newCmd(opts))
	}
	root.AddCommand(newWorkerCmd(opts))

	return root
}
