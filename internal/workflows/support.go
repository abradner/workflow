// Package workflows holds the five Temporal workflows this engine exposes -
// one per CLI command, each the direct replacement for a Ruby
// Workflow::Orchestrator. There's no equivalent of Ruby's generic
// Runner/Orchestrator/`needs` predicate framework here: each workflow is
// just an ordinary Go function that calls whatever activities it needs, in
// order. Temporal's workflow/activity split already gives us the
// hydrate/act/commit discipline the Ruby Runner had to hand-roll.
//
// The helpers below are thin wrappers over the platform's temporalutil
// package, kept so workflow code reads unqualified (`runActivity(...)`)
// at its many call sites.
package workflows

import (
	"go.temporal.io/sdk/workflow"

	"github.com/abradner/workflow/temporalutil"
)

func defaultActivityOptions() workflow.ActivityOptions {
	return temporalutil.DefaultActivityOptions()
}

// nonRetryingActivityOptions is for activities that create-but-don't-upsert
// a remote resource (e.g. a 1Password item) - the original Ruby tool never
// retried at all. See temporalutil.NonRetryingActivityOptions for why a
// retried create risks duplicates.
func nonRetryingActivityOptions() workflow.ActivityOptions {
	return temporalutil.NonRetryingActivityOptions()
}

// runActivity calls activityFn and unmarshals its result as T, instead of
// every call site declaring `var out T` and a separate Future.Get.
func runActivity[T any](ctx workflow.Context, activityFn any, args ...any) (T, error) {
	return temporalutil.RunActivity[T](ctx, activityFn, args...)
}
