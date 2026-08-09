package workflows

import (
	"errors"
	"fmt"

	"go.temporal.io/sdk/workflow"

	"github.com/abradner/workflow/internal/activities"
)

// Sync1PasswordInput is the `sync-1p` command's workflow input.
type Sync1PasswordInput struct {
	DryRun bool

	// Prune deletes vault fields this run did not write, instead of merely
	// reporting them. Set only by the `prune-1p` command - sync-1p never
	// enables it, and nothing infers it.
	Prune bool
}

// Sync1PasswordResult summarizes what the workflow did.
type Sync1PasswordResult struct {
	SecretsExtracted   int
	EnvironmentsSynced int
	DryRun             bool
}

// Sync1PasswordWorkflow fans out one Sync1PasswordEnvWorkflow child per
// target environment, each of which extracts the AWS secrets for
// Config.SourceEnv, remaps them onto its own environment (refreshing the
// Keycloak SAML public key where one is available), and, unless DryRun,
// provisions that environment's 1Password Secure Note.
//
// Unlike SyncWorkloads and SetupKeycloak, extraction is *not* shared across
// children by the parent - see the doc comment on activities.SyncEnvSecrets
// for why: sharing it would mean passing real AWS secret values through
// this parent workflow and into every child's input, both of which Temporal
// records in durable, plaintext event history. Each child re-extracts for
// itself instead, trading one extra AWS API round trip per environment for
// keeping actual secret values out of any workflow's visible history
// entirely - see docs/GO_NOTES.md's "Decomposing the monolith" section.
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

	// Fail before fanning out, not once per child. The activity keeps its own
	// guard as a backstop, but reaching it means every environment has already
	// started and fetched SAML credentials before failing identically - N
	// copies of one error, after N pointless round trips.
	if !in.DryRun && cfg.OPVaultName == "" {
		return Sync1PasswordResult{}, fmt.Errorf(
			"OP_VAULT_NAME is not set: sync-1p needs a target vault, or items land in the operator's personal vault")
	}

	// Fan out: one child per environment, started before waiting on any of
	// them so they run concurrently.
	logger.Info("Fanning out one child workflow per environment", "environments", cfg.Environments)
	futures := make([]workflow.ChildWorkflowFuture, len(cfg.Environments))
	for i, env := range cfg.Environments {
		futures[i] = workflow.ExecuteChildWorkflow(ctx, Sync1PasswordEnvWorkflow, Sync1PasswordEnvInput{
			ProjectName:      cfg.ProjectName,
			VaultName:        cfg.OPVaultName,
			ExactSecretNames: cfg.AdditionalExactSecrets,
			Prune:            in.Prune,
			SourceEnv:        cfg.SourceEnv,
			TargetEnv:        env,
			TLD:              cfg.TLD,
			DryRun:           in.DryRun,
		})
	}

	// Fan in: wait for every child and collect every failure instead of
	// returning on the first one.
	var secretsExtracted int
	var errs error
	for i, f := range futures {
		var envResult Sync1PasswordEnvResult
		if err := f.Get(ctx, &envResult); err != nil {
			errs = errors.Join(errs, fmt.Errorf("environment %s: %w", cfg.Environments[i], err))
			continue
		}
		secretsExtracted = envResult.SecretsExtracted
	}
	if errs != nil {
		return Sync1PasswordResult{}, errs
	}

	return Sync1PasswordResult{
		SecretsExtracted:   secretsExtracted,
		EnvironmentsSynced: len(cfg.Environments),
		DryRun:             in.DryRun,
	}, nil
}

// Sync1PasswordEnvInput is one environment's share of Sync1PasswordWorkflow's
// work - the unit Sync1PasswordWorkflow fans out over.
//
// This struct is serialized into Temporal event history as a child-workflow
// input, so **the field NAMES are wire format**. Renaming one is a versioning
// event: the name is the JSON key, and a child started by a previous release
// decodes the old key into the zero value when a new worker picks it up.
//
// The tempting dismissal is that such a run simply fails and gets re-run. It
// does not fail. It succeeds, with the field empty - and for ExactSecretNames
// that is silent data loss: the named secrets are never fetched, so their vault
// fields are not written this run, so they are classified stale, so `prune-1p`
// DELETES them. A benign-looking rename plus a destructive command is how a
// vault loses the ElastiCache credentials nobody realised were at risk.
//
// Hence the json tags below. They pin the wire names to what earlier releases
// wrote, so the Go name can say what the field holds without the wire name
// moving underneath a run already in flight. Do not "tidy" them away: the tag
// and the field name differing IS the point.
type Sync1PasswordEnvInput struct {
	ProjectName string
	VaultName   string

	// Wire name kept as the pre-rename "ExactSecrets" - see the type comment.
	ExactSecretNames []string `json:"ExactSecrets"`

	Prune     bool
	SourceEnv string
	TargetEnv string
	TLD       string
	DryRun    bool
}

// Sync1PasswordEnvResult summarizes what Sync1PasswordEnvWorkflow did for
// one environment.
type Sync1PasswordEnvResult struct {
	SecretsExtracted int
}

// Sync1PasswordEnvWorkflow fetches this environment's Keycloak SAML public
// key (if reachable), then extracts, remaps, and (unless DryRun) ingests
// this environment's 1Password Secure Note.
func Sync1PasswordEnvWorkflow(ctx workflow.Context, in Sync1PasswordEnvInput) (Sync1PasswordEnvResult, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	logger := workflow.GetLogger(ctx)
	var a *activities.Activities

	baseURL := fmt.Sprintf("https://pmn-keycloak.%s.%s.%s", in.ProjectName, in.TargetEnv, in.TLD)
	fetched, err := runActivity[activities.FetchSamlCredentialsResult](ctx, a.FetchSamlCredentials, activities.FetchSamlCredentialsInput{
		RealmName: "neons",
		BaseURL:   baseURL,
	})
	if err != nil {
		return Sync1PasswordEnvResult{}, fmt.Errorf("fetching SAML credentials: %w", err)
	}

	kcPublicKey := ""
	if fetched.Credentials != nil {
		kcPublicKey = fetched.Credentials.PEMPublicKey()
	}

	// Extraction, mapping, and 1Password ingestion all happen inside one
	// activity call - see the doc comment on activities.SyncEnvSecrets for
	// why the actual secret values must never cross back into workflow
	// code. Retries are disabled for the same reason IngestVaultItem always
	// was: the `op item create` call at the end of it isn't safe to retry.
	ingestCtx := workflow.WithActivityOptions(ctx, nonRetryingActivityOptions())
	synced, err := runActivity[activities.SyncEnvSecretsResult](ingestCtx, a.SyncEnvSecrets, activities.SyncEnvSecretsInput{
		ProjectName:      in.ProjectName,
		VaultName:        in.VaultName,
		ExactSecretNames: in.ExactSecretNames,
		Prune:            in.Prune,
		SourceEnv:        in.SourceEnv,
		TargetEnv:        in.TargetEnv,
		KCPublicKey:      kcPublicKey,
		DryRun:           in.DryRun,
	})
	if err != nil {
		return Sync1PasswordEnvResult{}, fmt.Errorf("syncing secrets: %w", err)
	}

	logger.Info("Synced secrets", "env", in.TargetEnv, "count", synced.SecretsExtracted)
	return Sync1PasswordEnvResult{SecretsExtracted: synced.SecretsExtracted}, nil
}
