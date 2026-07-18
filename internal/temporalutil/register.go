// Package temporalutil wires up this engine's two run modes: an embedded,
// in-process Temporal dev server for zero-dependency local CLI runs, and a
// thin client for dialing an existing (e.g. docker-compose) Temporal server
// that a separate long-lived `workflow worker` process is polling.
package temporalutil

import (
	"go.temporal.io/sdk/worker"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/workflows"
)

// TaskQueue is the single task queue this engine's worker(s) poll and every
// workflow execution targets.
const TaskQueue = "workflow-engine"

// RegisterAll registers every workflow and activity this engine defines - a
// worker built this way can run any of the five CLI commands.
func RegisterAll(r worker.Registry, acts *activities.Activities) {
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
}
