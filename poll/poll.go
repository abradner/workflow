// Package poll provides durable wait-for-completion loops for Temporal
// workflow code - the replacement for a CLI tool's `sleep N` busy-wait.
// The sleeps are workflow timers, so a wait survives worker restarts and
// replays deterministically; the budget is measured with workflow.Now for
// the same reason. Everything here is safe to call from workflow code and
// nothing here performs I/O - the check callback does that, via activities.
package poll

import (
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// budgetExhaustedType is the stable Temporal error type carried across
// workflow boundaries - the part of the error's identity that survives
// serialization.
const budgetExhaustedType = "PollBudgetExhausted"

// ErrBudgetExhausted reports that the budget ran out before check reported
// done. Until returns the last check's result alongside it, so the caller
// can report the last-known state ("still running") rather than nothing.
//
// Identity: use IsBudgetExhausted, not errors.Is. Within the workflow that
// called Until, errors.Is works; but when a child workflow returns this
// error, Temporal serializes it and the parent receives a reconstructed
// *temporal.ApplicationError - pointer identity is gone, and only the
// error's Type survives. IsBudgetExhausted checks both.
var ErrBudgetExhausted = temporal.NewApplicationError("poll: budget exhausted before completion", budgetExhaustedType)

// IsBudgetExhausted reports whether err is (or wraps) budget exhaustion,
// on either side of a workflow boundary.
func IsBudgetExhausted(err error) bool {
	if errors.Is(err, ErrBudgetExhausted) {
		return true
	}
	var appErr *temporal.ApplicationError
	return errors.As(err, &appErr) && appErr.Type() == budgetExhaustedType
}

// Until calls check immediately and then every interval until it reports
// done. The budget bounds SCHEDULING, not completion: no new sleep or check
// starts once it would overshoot, but a check already in flight runs to its
// own conclusion, so total workflow time is bounded by budget plus one
// check's duration - and the check's duration is the caller's to cap, via
// the activity options its activities run under. A caller needing a hard
// ceiling sets those timeouts accordingly. An immediate first check costs
// one extra activity when the answer is obviously "not yet" (e.g. right
// after triggering a build) - callers who know better can workflow.Sleep
// once before calling.
//
// check returns (result, done, err): err aborts the loop as a failure;
// done=true returns result as the answer. On budget exhaustion the last
// result is returned with ErrBudgetExhausted.
//
// interval must be positive: a zero or negative interval would busy-loop
// activity calls with no timer between them, growing event history as fast
// as the worker can go. A budget <= 0 is legal and means one check, no
// waiting.
func Until[T any](ctx workflow.Context, interval, budget time.Duration, check func(workflow.Context) (T, bool, error)) (T, error) {
	var zero T
	if interval <= 0 {
		return zero, fmt.Errorf("poll: interval must be positive, got %v", interval)
	}
	deadline := workflow.Now(ctx).Add(budget)

	for {
		result, done, err := check(ctx)
		if err != nil {
			return zero, err
		}
		if done {
			return result, nil
		}

		// Give up when the next wait would overshoot the deadline.
		// workflow.Now advances with activity completions too, so slow
		// checks consume budget just as sleeping does. This is a
		// scheduling bound: a check that straddles the deadline still
		// finishes (see the doc comment) - the loop schedules nothing
		// further after it.
		if workflow.Now(ctx).Add(interval).After(deadline) {
			return result, ErrBudgetExhausted
		}
		if err := workflow.Sleep(ctx, interval); err != nil {
			return zero, err
		}
	}
}
