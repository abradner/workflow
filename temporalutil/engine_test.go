package temporalutil_test

import (
	"context"
	"testing"

	"go.temporal.io/sdk/worker"

	"github.com/stretchr/testify/assert"
	"go.temporal.io/server/common/namespace"

	"github.com/abradner/workflow/temporalutil"
)

// HistoryRetention is pinned to the server's own hard floor rather than a
// number someone liked. If a server upgrade moves that floor, this fails
// rather than silently leaving history around longer than intended.
func TestHistoryRetention_MatchesTheServersLocalMinimum(t *testing.T) {
	assert.Equal(t, namespace.MinRetentionLocal, temporalutil.HistoryRetention,
		"retention should track the shortest window the server accepts")
}

// Every execution is bounded. A batch job still running an hour later is
// stuck, and should fail rather than sit in history holding its inputs.
func TestStartOptions_BoundsEveryRun(t *testing.T) {
	opts := temporalutil.StartOptions("some-queue")

	assert.Equal(t, "some-queue", opts.TaskQueue,
		"StartOptions must target the queue the engine's workers poll")
	assert.Equal(t, temporalutil.WorkflowRunTimeout, opts.WorkflowRunTimeout,
		"StartOptions must use the package's configured timeout, whatever it becomes")
	assert.NotZero(t, opts.WorkflowRunTimeout, "an unbounded run has no history TTL either")
}

// A zero-value Engine (e.g. an Options built outside cli.New) must fail with
// a description, not a panic deep inside worker registration.
func TestEngineValidate_RejectsIncompleteEngines(t *testing.T) {
	register := func(context.Context, worker.Registry) error { return nil }

	assert.ErrorContains(t, temporalutil.Engine{}.Validate(), "TaskQueue")
	assert.ErrorContains(t, temporalutil.Engine{TaskQueue: "q"}.Validate(), "Register")
	assert.ErrorContains(t, temporalutil.Engine{Register: register}.Validate(), "TaskQueue",
		"missing queue is reported even when Register is set")
	assert.NoError(t, temporalutil.Engine{TaskQueue: "q", Register: register}.Validate())
}
