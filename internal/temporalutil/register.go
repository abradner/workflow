// Package temporalutil wires up this engine's two run modes: an embedded,
// in-process Temporal dev server for zero-dependency local CLI runs, and a
// thin client for dialing an existing (e.g. docker-compose) Temporal server
// that a separate long-lived `workflow worker` process is polling.
package temporalutil

import (
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/workflows"
)

// TaskQueue is the single task queue this engine's worker(s) poll and every
// workflow execution targets.
const TaskQueue = "workflow-engine"

// HistoryRetention is how long a completed workflow's event history is kept
// before the server deletes it.
//
// One hour is not a round number picked for taste: it is the hard floor the
// server permits for a local namespace (namespace.MinRetentionLocal). Zero is
// rejected, because it would be ambiguous with "keep forever", and global
// (replicated) namespaces are held to 24h to allow for replication. So this is
// the shortest window Temporal will accept for a deployment shaped like ours.
//
// It matters because event history is durable, readable, plaintext storage.
// The design keeps secret *values* out of it, but what remains is not nothing:
// environment names, item titles, paths, counts. Retention does not make that
// safe, it makes it short-lived - a genuine mitigation and an explicitly
// partial one. See docs/ARCHITECTURE.md for what would actually fix it.
const HistoryRetention = time.Hour

// WorkflowRunTimeout bounds a single run. Every workflow here is a batch job
// measured in seconds to minutes; one still executing an hour later is stuck,
// not slow, and should fail rather than sit in history holding its inputs.
const WorkflowRunTimeout = time.Hour

// StartOptions are the options every workflow execution starts with.
func StartOptions() client.StartWorkflowOptions {
	return client.StartWorkflowOptions{
		TaskQueue:          TaskQueue,
		WorkflowRunTimeout: WorkflowRunTimeout,
	}
}

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
