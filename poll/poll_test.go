package poll_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/abradner/workflow/poll"
)

// probeResult stands in for a consumer's build-status result.
type probeResult struct {
	Status string
}

// probe is the activity the test workflow polls; its responses are mocked
// per test.
func probe(context.Context) (probeResult, error) {
	return probeResult{}, errors.New("always mocked")
}

// pollingWorkflow polls probe every 10s with the given budget and returns
// the final status (joined with poll's error, if any, via the error path).
func pollingWorkflow(ctx workflow.Context, budget time.Duration) (probeResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		// No activity retries: each mocked response must be consumed exactly
		// once, or the poll-count assertions would be counting SDK retries.
		RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	return poll.Until(ctx, 10*time.Second, budget, func(ctx workflow.Context) (probeResult, bool, error) {
		var r probeResult
		if err := workflow.ExecuteActivity(ctx, probe).Get(ctx, &r); err != nil {
			return r, false, err
		}
		return r, r.Status == "success", nil
	})
}

func newEnv(t *testing.T) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(probe)
	return env
}

func TestUntil_PollsThroughTimersUntilDone(t *testing.T) {
	env := newEnv(t)
	env.OnActivity(probe, mock.Anything).Return(probeResult{Status: "running"}, nil).Twice()
	env.OnActivity(probe, mock.Anything).Return(probeResult{Status: "success"}, nil).Once()

	env.ExecuteWorkflow(pollingWorkflow, time.Hour)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result probeResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "success", result.Status)
	env.AssertExpectations(t)
}

func TestUntil_BudgetExhaustionCarriesTheLastKnownState(t *testing.T) {
	env := newEnv(t)
	// Never succeeds. 25s budget at 10s interval = checks at t=0, t=10, t=20,
	// then the next sleep would overshoot, so exactly 3 checks.
	env.OnActivity(probe, mock.Anything).Return(probeResult{Status: "running"}, nil).Times(3)

	env.ExecuteWorkflow(pollingWorkflow, 25*time.Second)

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.ErrorContains(t, err, poll.ErrBudgetExhausted.Error(),
		"exhaustion must be distinguishable from a check failure")
	env.AssertExpectations(t)
}

func TestUntil_ACheckErrorAbortsTheLoop(t *testing.T) {
	env := newEnv(t)
	env.OnActivity(probe, mock.Anything).Return(probeResult{}, errors.New("gitlab exploded")).Once()

	env.ExecuteWorkflow(pollingWorkflow, time.Hour)

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.ErrorContains(t, err, "gitlab exploded")
	env.AssertExpectations(t)
}

func TestUntil_RejectsANonPositiveInterval(t *testing.T) {
	env := newEnv(t)
	// No mocked probe responses: a zero interval must fail before any check
	// runs - the alternative is a timerless busy-loop of activity calls.
	env.ExecuteWorkflow(func(ctx workflow.Context) (probeResult, error) {
		return poll.Until(ctx, 0, time.Hour, func(workflow.Context) (probeResult, bool, error) {
			return probeResult{}, false, nil
		})
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.ErrorContains(t, err, "interval must be positive")
	env.AssertExpectations(t)
}
