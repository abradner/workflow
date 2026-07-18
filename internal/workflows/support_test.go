package workflows_test

import (
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
)

// mockLoadConfig sets up the LoadConfig activity every workflow calls first,
// so each test doesn't have to repeat this - it stands in for the .env
// loading a real worker process would do.
func mockLoadConfig(env *testsuite.TestWorkflowEnvironment, a *activities.Activities, cfg config.Config) {
	env.OnActivity(a.LoadConfig, mock.Anything).Return(activities.LoadConfigResult{Config: cfg}, nil)
}
