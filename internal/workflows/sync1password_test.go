package workflows_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/domain"
	"github.com/abradner/workflow/internal/workflows"
)

func strptr(s string) *string { return &s }

func TestSync1PasswordWorkflow_MapsAndIngestsPerEnvironment(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var a *activities.Activities

	cfg := config.Config{
		SourceEnv:    "dev4",
		Environments: []string{"dev4", "dev5"},
		ProjectName:  "pmn",
		TLD:          "f-ck.xyz",
	}

	env.OnActivity(a.FetchSamlCredentials, mock.Anything, mock.MatchedBy(func(in activities.FetchSamlCredentialsInput) bool {
		return in.BaseURL == "https://pmn-keycloak.pmn.dev4.f-ck.xyz"
	})).Return(activities.FetchSamlCredentialsResult{Credentials: &domain.SamlCredentials{PublicKey: "fresh_key", SSOXML: "<xml/>"}}, nil)

	env.OnActivity(a.FetchSamlCredentials, mock.Anything, mock.MatchedBy(func(in activities.FetchSamlCredentialsInput) bool {
		return in.BaseURL == "https://pmn-keycloak.pmn.dev5.f-ck.xyz"
	})).Return(activities.FetchSamlCredentialsResult{Credentials: nil}, nil)

	env.OnActivity(a.ExtractAWSSecrets, mock.Anything, activities.ExtractAWSSecretsInput{Env: "dev4"}).
		Return(activities.ExtractAWSSecretsResult{Secrets: []domain.ExtractedSecret{
			{Name: "dev4/pmn-ui-api-config", String: strptr(`{"mp.jwt.verify.publickey":"stale"}`)},
		}}, nil)

	ingested := map[string][]domain.ExtractedSecret{}
	env.OnActivity(a.IngestVaultItem, mock.Anything, mock.MatchedBy(func(in activities.IngestVaultItemInput) bool {
		ingested[in.Env] = in.Secrets
		return true
	})).Return(nil)

	env.ExecuteWorkflow(workflows.Sync1PasswordWorkflow, workflows.Sync1PasswordInput{Config: cfg, DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result workflows.Sync1PasswordResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 1, result.SecretsExtracted)
	require.Equal(t, 2, result.EnvironmentsSynced)

	require.Len(t, ingested["dev4"], 1)
	var dev4Payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(*ingested["dev4"][0].String), &dev4Payload))
	expectedPEM := domain.SamlCredentials{PublicKey: "fresh_key"}.PEMPublicKey()
	require.Equal(t, expectedPEM, dev4Payload["mp.jwt.verify.publickey"], "dev4 has fresh Keycloak credentials to inject, PEM-formatted")

	require.Len(t, ingested["dev5"], 1)
	var dev5Payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(*ingested["dev5"][0].String), &dev5Payload))
	require.Equal(t, "stale", dev5Payload["mp.jwt.verify.publickey"], "dev5 had no reachable Keycloak, so the stale key is left alone")
}

func TestSync1PasswordWorkflow_DryRunSkipsIngestion(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var a *activities.Activities

	cfg := config.Config{SourceEnv: "dev4", Environments: []string{"dev4"}, ProjectName: "pmn", TLD: "f-ck.xyz"}

	env.OnActivity(a.FetchSamlCredentials, mock.Anything, mock.Anything).
		Return(activities.FetchSamlCredentialsResult{Credentials: nil}, nil)
	env.OnActivity(a.ExtractAWSSecrets, mock.Anything, mock.Anything).
		Return(activities.ExtractAWSSecretsResult{Secrets: []domain.ExtractedSecret{{Name: "dev4/x", String: strptr("y")}}}, nil)
	// No IngestVaultItem mock: the test environment fails the test if the
	// workflow tries to call an activity with no matching expectation.

	env.ExecuteWorkflow(workflows.Sync1PasswordWorkflow, workflows.Sync1PasswordInput{Config: cfg, DryRun: true})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

// TestSync1PasswordWorkflow_IngestFailureIsNotRetried is the regression test
// for the "op item create isn't idempotent" fix: IngestVaultItem must run
// with retries disabled, unlike every other activity in this workflow, so a
// failure never risks creating a duplicate 1Password item behind the scenes.
func TestSync1PasswordWorkflow_IngestFailureIsNotRetried(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var a *activities.Activities

	cfg := config.Config{SourceEnv: "dev4", Environments: []string{"dev4"}, ProjectName: "pmn", TLD: "f-ck.xyz"}

	env.OnActivity(a.FetchSamlCredentials, mock.Anything, mock.Anything).
		Return(activities.FetchSamlCredentialsResult{Credentials: nil}, nil)
	env.OnActivity(a.ExtractAWSSecrets, mock.Anything, mock.Anything).
		Return(activities.ExtractAWSSecretsResult{Secrets: []domain.ExtractedSecret{{Name: "dev4/x", String: strptr("y")}}}, nil)
	env.OnActivity(a.IngestVaultItem, mock.Anything, mock.Anything).
		Return(errors.New("op item create: some transient failure"))

	env.ExecuteWorkflow(workflows.Sync1PasswordWorkflow, workflows.Sync1PasswordInput{Config: cfg, DryRun: false})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	env.AssertActivityNumberOfCalls(t, "IngestVaultItem", 1)
}
