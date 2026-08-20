// Command workflow is the CLI entry point: the direct replacement for the
// original tool's workflow.rb. Each subcommand starts the matching Temporal
// workflow and waits for it to finish, either against an embedded
// in-process Temporal server (the default) or an external one (see the
// --temporal flag and the "worker" subcommand, both provided by the
// platform's cli package).
package main

import (
	"os"

	"github.com/abradner/workflow/cli"
)

func main() {
	app := cli.App{
		Name:   "workflow",
		Short:  "A Go + Temporal ETL pipeline for migrating, transforming and adapting Kubernetes workloads",
		Engine: engine,
	}

	root := cli.New(app,
		newSyncCmd,
		newSetupArgoCmd,
		newSync1PasswordCmd,
		newPrune1PasswordCmd,
		newRenderTalosCmd,
		newSetupKeycloakCmd,
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
