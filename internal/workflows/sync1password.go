package workflows

import (
	"fmt"

	"go.temporal.io/sdk/workflow"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/domain"
	"github.com/abradner/workflow/internal/transformers"
)

// Sync1PasswordInput is the `sync-1p` command's workflow input.
type Sync1PasswordInput struct {
	DryRun bool
}

// Sync1PasswordResult summarizes what the workflow did.
type Sync1PasswordResult struct {
	SecretsExtracted   int
	EnvironmentsSynced int
	DryRun             bool
}

// Sync1PasswordWorkflow extracts every AWS secret for Config.SourceEnv,
// remaps it onto each target environment (refreshing the Keycloak SAML
// public key where one is available), and (unless DryRun) provisions one
// 1Password Secure Note per environment.
func Sync1PasswordWorkflow(ctx workflow.Context, in Sync1PasswordInput) (Sync1PasswordResult, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	logger := workflow.GetLogger(ctx)
	var a *activities.Activities

	// Config is loaded on whichever machine runs the worker, not wherever
	// this workflow was started from - see the doc comment on LoadConfig.
	cfgResult, err := runActivity[activities.LoadConfigResult](ctx, a.LoadConfig)
	if err != nil {
		return Sync1PasswordResult{}, fmt.Errorf("loading config: %w", err)
	}
	cfg := cfgResult.Config

	// Hydration: fetch SAML credentials for every target environment. This
	// always runs, dry-run or not - Ruby's Runner hydrates unconditionally
	// and only ever skips the commit phase.
	credentialsByEnv := map[string]*domain.SamlCredentials{}
	for _, env := range cfg.Environments {
		baseURL := fmt.Sprintf("https://pmn-keycloak.%s.%s.%s", cfg.ProjectName, env, cfg.TLD)

		fetched, err := runActivity[activities.FetchSamlCredentialsResult](ctx, a.FetchSamlCredentials, activities.FetchSamlCredentialsInput{
			RealmName: "neons",
			BaseURL:   baseURL,
		})
		if err != nil {
			return Sync1PasswordResult{}, fmt.Errorf("fetching SAML credentials for %s: %w", env, err)
		}
		credentialsByEnv[env] = fetched.Credentials
	}

	logger.Info("Extracting AWS secrets", "sourceEnv", cfg.SourceEnv)
	extracted, err := runActivity[activities.ExtractAWSSecretsResult](ctx, a.ExtractAWSSecrets, activities.ExtractAWSSecretsInput{Env: cfg.SourceEnv})
	if err != nil {
		return Sync1PasswordResult{}, fmt.Errorf("extracting AWS secrets: %w", err)
	}
	logger.Info("Extracted secrets from AWS", "count", len(extracted.Secrets))
	logger.Info("Mapping 1Password items", "environments", cfg.Environments)

	mappedByEnv := map[string][]domain.ExtractedSecret{}
	for _, env := range cfg.Environments {
		kcPublicKey := ""
		if creds := credentialsByEnv[env]; creds != nil {
			kcPublicKey = creds.PEMPublicKey()
		}

		mapper := transformers.OnePasswordSamlKeyInjector{
			SourceEnv:   cfg.SourceEnv,
			TargetEnv:   env,
			KCPublicKey: kcPublicKey,
			Logger:      logger,
		}
		mappedByEnv[env] = mapper.Call(extracted.Secrets)
	}

	result := Sync1PasswordResult{
		SecretsExtracted:   len(extracted.Secrets),
		EnvironmentsSynced: len(cfg.Environments),
		DryRun:             in.DryRun,
	}

	if in.DryRun {
		logger.Info("Dry run: skipping 1Password commit phase")
		return result, nil
	}

	// IngestVaultItem shells out to `op item create`, which has no
	// idempotency key or upsert path - every call makes a brand new item.
	// Run it without the default retry policy so a transient failure after
	// a successful remote create can't get retried into a duplicate item.
	ingestCtx := workflow.WithActivityOptions(ctx, nonRetryingActivityOptions())

	for _, env := range cfg.Environments {
		logger.Info("Pushing 1Password vault item", "item", fmt.Sprintf("k8s-%s-%s", cfg.ProjectName, env))

		err := workflow.ExecuteActivity(ingestCtx, a.IngestVaultItem, activities.IngestVaultItemInput{
			ProjectName: cfg.ProjectName,
			Env:         env,
			Secrets:     mappedByEnv[env],
		}).Get(ingestCtx, nil)
		if err != nil {
			return Sync1PasswordResult{}, fmt.Errorf("ingesting vault item for %s: %w", env, err)
		}
	}

	return result, nil
}
