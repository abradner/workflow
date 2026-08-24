// Package poll provides durable wait-for-completion loops for Temporal
// workflow code - the replacement for a CLI tool's `sleep N` busy-wait.
// The sleeps are workflow timers, so a wait survives worker restarts and
// replays deterministically; the budget is measured with workflow.Now for
// the same reason. Everything here is safe to call from workflow code and
// nothing here performs I/O - the check callback does that, via activities.
package poll

import (
	"errors"
	"time"

	"go.temporal.io/sdk/workflow"
)

// ErrBudgetExhausted reports that the budget ran out before check reported
// done. Callers distinguish it from a check failure with errors.Is; Until
// returns the last check's result alongside it, so the caller can report
// the last-known state ("still running") rather than nothing.
var ErrBudgetExhausted = errors.New("poll: budget exhausted before completion")

// Until calls check immediately and then every interval until it reports
// done, for at most budget of total waiting. An immediate first check costs
// one extra activity when the answer is obviously "not yet" (e.g. right
// after triggering a build) - callers who know better can workflow.Sleep
// once before calling.
//
// check returns (result, done, err): err aborts the loop as a failure;
// done=true returns result as the answer. On budget exhaustion the last
// result is returned with ErrBudgetExhausted.
func Until[T any](ctx workflow.Context, interval, budget time.Duration, check func(workflow.Context) (T, bool, error)) (T, error) {
	var zero T
	deadline := workflow.Now(ctx).Add(budget)

	for {
		result, done, err := check(ctx)
		if err != nil {
			return zero, err
		}
		if done {
			return result, nil
		}

		// Give up when the next wait would overshoot the budget: total
		// sleeping never exceeds it.
		if workflow.Now(ctx).Add(interval).After(deadline) {
			return result, ErrBudgetExhausted
		}
		if err := workflow.Sleep(ctx, interval); err != nil {
			return zero, err
		}
	}
}
