package main

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/worker"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/workflows"
	"github.com/abradner/workflow/temporalutil"
)

// engine is this tool's Temporal surface: the task queue its workers poll
// and the registration of every workflow and activity it defines. Both run
// modes (embedded and external) and the worker subcommand consume this one
// definition.
var engine = temporalutil.Engine{
	TaskQueue: "workflow-engine",
	Register:  registerAll,
}

// registerAll registers every workflow and activity this engine defines - a
// worker built this way can run any of the CLI commands.
func registerAll(ctx context.Context, r worker.Registry) error {
	acts, err := activities.New(ctx)
	if err != nil {
		return fmt.Errorf("building activities: %w", err)
	}

	r.RegisterWorkflow(workflows.SyncWorkloadsWorkflow)
	r.RegisterWorkflow(workflows.SyncAppWorkflow)
	r.RegisterWorkflow(workflows.GenerateArgocdWorkflow)
	r.RegisterWorkflow(workflows.Sync1PasswordWorkflow)
	r.RegisterWorkflow(workflows.Sync1PasswordEnvWorkflow)
	r.RegisterWorkflow(workflows.RenderTalosWorkflow)
	r.RegisterWorkflow(workflows.SetupKeycloakWorkflow)
	r.RegisterWorkflow(workflows.SetupKeycloakEnvWorkflow)

	// Activities is a struct pointer, so this registers every one of its
	// exported methods (DiscoverApps, BuildAppFiles, ...) as an activity
	// named after the method - no need to list them one by one.
	r.RegisterActivity(acts)
	return nil
}
