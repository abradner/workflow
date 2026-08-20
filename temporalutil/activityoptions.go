package temporalutil

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// DefaultActivityOptions is the platform's baseline for activity calls:
// bounded, with a short exponential retry for transient failures.
func DefaultActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    5,
		},
	}
}

// NonRetryingActivityOptions is for activities that create-but-don't-upsert
// a remote resource - retrying a create-only call risks producing a
// duplicate if the create succeeded remotely but the activity failed to
// report completion (a timeout, a worker crash) before Temporal recorded it.
// MaximumAttempts: 1 is the SDK's documented way to disable retries.
func NonRetryingActivityOptions() workflow.ActivityOptions {
	opts := DefaultActivityOptions()
	opts.RetryPolicy = &temporal.RetryPolicy{MaximumAttempts: 1}
	return opts
}

// RunActivity calls activityFn and unmarshals its result as T, instead of
// every call site declaring `var out T` and a separate Future.Get.
func RunActivity[T any](ctx workflow.Context, activityFn any, args ...any) (T, error) {
	var result T
	err := workflow.ExecuteActivity(ctx, activityFn, args...).Get(ctx, &result)
	return result, err
}
