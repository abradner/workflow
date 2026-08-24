// Package temporalutil wires up a consumer application's two run modes: an
// embedded, in-process Temporal dev server for zero-dependency local CLI
// runs, and a thin client for dialing an existing (e.g. docker-compose)
// Temporal server that a separate long-lived worker process is polling.
//
// This package is platform code: it knows nothing about any consumer's
// workflows or activities. A consumer describes its Temporal surface once,
// as an Engine, and passes it to RunEmbedded / RunExternal / RunWorker.
package temporalutil

import (
	"context"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// Engine describes one consumer application's Temporal surface: the task
// queue its workers poll (and every workflow execution targets), and the
// registration of all its workflows and activities.
type Engine struct {
	// TaskQueue is the single task queue this engine's worker(s) poll and
	// every workflow execution targets.
	TaskQueue string

	// Register registers every workflow and activity the engine defines,
	// building any effectful dependencies (service clients, config) from
	// ctx. A worker registered this way can run any of the consumer's
	// commands. Called once per worker, on the machine the worker runs on.
	Register func(ctx context.Context, r worker.Registry) error
}

// HistoryRetention is the intended namespace retention for engines built on
// this package.
//
// It configures NOTHING on its own. Retention is a server-side namespace
// setting; this constant exists to name the intended value in one place and to
// be asserted against the server's own floor. The value that actually takes
// effect is set where the namespace is created - DEFAULT_NAMESPACE_RETENTION
// in a consumer's docker-compose for external mode. Change one, change the
// other; the drift test in engine_test.go catches drift from the floor but
// cannot catch drift from compose.
//
// One hour is not a round number picked for taste: it is the hard floor the
// server permits for a local namespace (namespace.MinRetentionLocal). Zero is
// rejected, because it would be ambiguous with "keep forever", and global
// (replicated) namespaces are held to 24h to allow for replication. So this is
// the shortest window Temporal will accept for a deployment shaped like ours.
//
// It matters because event history is durable, readable, plaintext storage.
// The platform's design rules keep secret *values* out of it, but what remains
// is not nothing: environment names, item titles, paths, counts. Retention
// does not make that safe, it makes it short-lived - a genuine mitigation and
// an explicitly partial one. See docs/ARCHITECTURE.md for what would actually
// fix it.
const HistoryRetention = time.Hour

// WorkflowRunTimeout bounds a single run. Every workflow this platform is
// built for is a batch job measured in seconds to minutes; one still executing
// an hour later is stuck, not slow, and should fail rather than sit in history
// holding its inputs. Workflows that legitimately outlive this (e.g. polling a
// long external build) should be started with their own explicit timeout
// rather than moving this default.
const WorkflowRunTimeout = time.Hour

// StartOptions are the options ROOT workflow executions start with - the ones
// RunEmbedded and RunExternal launch. Child workflows inherit their parent's
// deadline rather than these, and testsuite executions bypass them entirely,
// so this is not a global policy hook.
func StartOptions(taskQueue string) client.StartWorkflowOptions {
	return client.StartWorkflowOptions{
		TaskQueue:          taskQueue,
		WorkflowRunTimeout: WorkflowRunTimeout,
	}
}
