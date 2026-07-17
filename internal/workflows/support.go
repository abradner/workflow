// Package workflows holds the five Temporal workflows this engine exposes -
// one per CLI command, each the direct replacement for a Ruby
// Workflow::Orchestrator. There's no equivalent of Ruby's generic
// Runner/Orchestrator/`needs` predicate framework here: each workflow is
// just an ordinary Go function that calls whatever activities it needs, in
// order. Temporal's workflow/activity split already gives us the
// hydrate/act/commit discipline the Ruby Runner had to hand-roll.
package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

func defaultActivityOptions() workflow.ActivityOptions {
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

// runActivity calls activityFn and unmarshals its result as T, instead of
// every call site declaring `var out T` and a separate Future.Get.
func runActivity[T any](ctx workflow.Context, activityFn any, args ...any) (T, error) {
	var result T
	err := workflow.ExecuteActivity(ctx, activityFn, args...).Get(ctx, &result)
	return result, err
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, i := range items {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	return out
}
