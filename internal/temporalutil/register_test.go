package temporalutil_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.temporal.io/server/common/namespace"

	"github.com/abradner/workflow/internal/temporalutil"
)

// HistoryRetention is pinned to the server's own hard floor rather than a
// number someone liked. If a server upgrade moves that floor, this fails
// rather than silently leaving history around longer than intended.
func TestHistoryRetention_MatchesTheServersLocalMinimum(t *testing.T) {
	assert.Equal(t, namespace.MinRetentionLocal, temporalutil.HistoryRetention,
		"retention should track the shortest window the server accepts")
	assert.Equal(t, time.Hour, temporalutil.HistoryRetention)
}

// Every execution is bounded. A batch job still running an hour later is
// stuck, and should fail rather than sit in history holding its inputs.
func TestStartOptions_BoundsEveryRun(t *testing.T) {
	opts := temporalutil.StartOptions()

	assert.Equal(t, temporalutil.TaskQueue, opts.TaskQueue)
	assert.Equal(t, time.Hour, opts.WorkflowRunTimeout)
	assert.NotZero(t, opts.WorkflowRunTimeout, "an unbounded run has no history TTL either")
}
