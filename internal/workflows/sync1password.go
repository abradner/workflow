package workflows

import (
	"errors"
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

// Sync1PasswordWorkflow extracts every AWS secret for Config.SourceEnv once,
// then fans out one Sync1PasswordEnvWorkflow child per target environment to
// remap it (refreshing the Keycloak SAML public key where one is available)
// and, unless DryRun, provision that environment's 1Password Secure Note.
//
// Extraction stays a single shared step - it doesn't depend on which
// environment is being synced, so there's nothing to gain from repeating it
// per child. Everything downstream of it (credential lookup, mapping,
// ingestion) is entirely per-environment, so those run concurrently as
// child workflows - see docs/GO_NOTES.md's "Decomposing the monolith"
// section.
//
// Unlike SetupKeycloakWorkflow, one environment's failure here still fails
// the whole run - matching the original Ruby commit_phase, which had no
// per-environment rescue (a single `each` loop that aborts on the first
// error). Fanning out to children changes *when* that shows up: every
// environment's child now runs to completion concurrently and every
// failure is reported, rather than the original stopping dead at the first
// one - but the overall pass/fail contract is unchanged: any environment
// failing still fails the run.
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

	logger.Info("Extracting AWS secrets", "sourceEnv", cfg.SourceEnv)
	extracted, err := runActivity[activities.ExtractAWSSecretsResult](ctx, a.ExtractAWSSecrets, activities.ExtractAWSSecretsInput{Env: cfg.SourceEnv})
	if err != nil {
		return Sync1PasswordResult{}, fmt.Errorf("extracting AWS secrets: %w", err)
	}
	logger.Info("Extracted secrets from AWS", "count", len(extracted.Secrets))

	// Fan out: one child per environment, started before waiting on any of
	// them so they run concurrently.
	logger.Info("Fanning out one child workflow per environment", "environments", cfg.Environments)
	futures := make([]workflow.ChildWorkflowFuture, len(cfg.Environments))
	for i, env := range cfg.Environments {
		futures[i] = workflow.ExecuteChildWorkflow(ctx, Sync1PasswordEnvWorkflow, Sync1PasswordEnvInput{
			ProjectName: cfg.ProjectName,
			SourceEnv:   cfg.SourceEnv,
			TargetEnv:   env,
			TLD:         cfg.TLD,
			Secrets:     extracted.Secrets,
			DryRun:      in.DryRun,
		})
	}

	// Fan in: wait for every child and collect every failure instead of
	// returning on the first one.
	var errs error
	for i, f := range futures {
		if err := f.Get(ctx, nil); err != nil {
			errs = errors.Join(errs, fmt.Errorf("environment %s: %w", cfg.Environments[i], err))
		}
	}
	if errs != nil {
		return Sync1PasswordResult{}, errs
	}

	return Sync1PasswordResult{
		SecretsExtracted:   len(extracted.Secrets),
		EnvironmentsSynced: len(cfg.Environments),
		DryRun:             in.DryRun,
	}, nil
}

// Sync1PasswordEnvInput is one environment's share of Sync1PasswordWorkflow's
// work - the unit Sync1PasswordWorkflow fans out over. Secrets is the AWS
// secrets already extracted once by the parent for Config.SourceEnv.
type Sync1PasswordEnvInput struct {
	ProjectName string
	SourceEnv   string
	TargetEnv   string
	TLD         string
	Secrets     []domain.ExtractedSecret
	DryRun      bool
}

// Sync1PasswordEnvWorkflow fetches this environment's Keycloak SAML public
// key (if reachable), remaps the shared extracted secrets onto this
// environment, and, unless DryRun, provisions its 1Password Secure Note.
func Sync1PasswordEnvWorkflow(ctx workflow.Context, in Sync1PasswordEnvInput) error {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	logger := workflow.GetLogger(ctx)
	var a *activities.Activities

	baseURL := fmt.Sprintf("https://pmn-keycloak.%s.%s.%s", in.ProjectName, in.TargetEnv, in.TLD)
	fetched, err := runActivity[activities.FetchSamlCredentialsResult](ctx, a.FetchSamlCredentials, activities.FetchSamlCredentialsInput{
		RealmName: "neons",
		BaseURL:   baseURL,
	})
	if err != nil {
		return fmt.Errorf("fetching SAML credentials: %w", err)
	}

	kcPublicKey := ""
	if fetched.Credentials != nil {
		kcPublicKey = fetched.Credentials.PEMPublicKey()
	}

	mapper := transformers.OnePasswordSamlKeyInjector{
		SourceEnv:   in.SourceEnv,
		TargetEnv:   in.TargetEnv,
		KCPublicKey: kcPublicKey,
		Logger:      logger,
	}
	mapped := mapper.Call(in.Secrets)

	if in.DryRun {
		logger.Info("Dry run: skipping 1Password commit", "env", in.TargetEnv)
		return nil
	}

	// IngestVaultItem shells out to `op item create`, which has no
	// idempotency key or upsert path - every call makes a brand new item.
	// Run it without the default retry policy so a transient failure after
	// a successful remote create can't get retried into a duplicate item.
	ingestCtx := workflow.WithActivityOptions(ctx, nonRetryingActivityOptions())

	logger.Info("Pushing 1Password vault item", "item", fmt.Sprintf("k8s-%s-%s", in.ProjectName, in.TargetEnv))
	if err := workflow.ExecuteActivity(ingestCtx, a.IngestVaultItem, activities.IngestVaultItemInput{
		ProjectName: in.ProjectName,
		Env:         in.TargetEnv,
		Secrets:     mapped,
	}).Get(ingestCtx, nil); err != nil {
		return fmt.Errorf("ingesting vault item: %w", err)
	}

	return nil
}
