// Command workflow is the CLI entry point: the direct replacement for the
// original tool's workflow.rb. Each subcommand starts the matching Temporal
// workflow and waits for it to finish, either against an embedded
// in-process Temporal server (the default) or an external one (see the
// --temporal flag and the "worker" subcommand).
package main

import "os"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
