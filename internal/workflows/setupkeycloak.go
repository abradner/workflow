package workflows

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
)

const (
	keycloakReadyMaxAttempts = 12
	keycloakReadyPollDelay   = 5 * time.Second
)

// SetupKeycloakInput is the `setup-keycloak` command's workflow input.
type SetupKeycloakInput struct {
	DryRun bool
}

// SetupKeycloakResult summarizes what the workflow did.
type SetupKeycloakResult struct {
	EnvironmentsAttempted int
	EnvironmentsSucceeded int
	DryRun                bool
}

// SetupKeycloakWorkflow fans out one SetupKeycloakEnvWorkflow child per
// target environment, each of which waits for that environment's Keycloak
// to come up, provisions the "neons" realm (OIDC + SAML clients, groups,
// seed users), and writes out its exported SAML descriptor.
//
// Unlike the other four workflows, essentially all of this tool's real work
// happens in what Ruby called the "commit phase" - so under DryRun it does
// nothing at all beyond logging the plan, exactly like the original.
//
// One environment's failure doesn't stop the others - matches Ruby's
// per-environment rescue in SetupKeycloak#commit_phase. Running each
// environment as its own child workflow gets that isolation "for free" (a
// failed child just reports its own error to the parent) while also
// letting every environment's readiness poll and provisioning run
// concurrently instead of one after another, the way the original
// sequential loop did - see docs/GO_NOTES.md's "Decomposing the monolith"
// section.
func SetupKeycloakWorkflow(ctx workflow.Context, in SetupKeycloakInput) (SetupKeycloakResult, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	logger := workflow.GetLogger(ctx)
	var a *activities.Activities

	// Config is loaded on whichever machine runs the worker, not wherever
	// this workflow was started from - see the doc comment on LoadConfig.
	cfgResult, err := runActivity[activities.LoadConfigResult](ctx, a.LoadConfig)
	if err != nil {
		return SetupKeycloakResult{}, fmt.Errorf("loading config: %w", err)
	}
	cfg := cfgResult.Config

	logger.Info("Planning Keycloak setup", "environments", strings.Join(cfg.Environments, ", "))

	result := SetupKeycloakResult{EnvironmentsAttempted: len(cfg.Environments), DryRun: in.DryRun}

	if in.DryRun {
		logger.Info("Dry run: SetupKeycloak's provisioning work all lives in the commit phase, skipping entirely")
		return result, nil
	}

	// Fan out: one child per environment, started before waiting on any of
	// them so they run concurrently.
	futures := make([]workflow.ChildWorkflowFuture, len(cfg.Environments))
	for i, env := range cfg.Environments {
		futures[i] = workflow.ExecuteChildWorkflow(ctx, SetupKeycloakEnvWorkflow, SetupKeycloakEnvInput{
			Config: cfg,
			Env:    env,
		})
	}

	// Fan in: a failed child only ever reports its own environment's error -
	// it can't stop its siblings, which have already been scheduled.
	for i, f := range futures {
		env := cfg.Environments[i]
		if err := f.Get(ctx, nil); err != nil {
			logger.Error("Failed to setup Keycloak", "env", env, "error", err.Error())
			continue
		}
		result.EnvironmentsSucceeded++
	}

	return result, nil
}

// SetupKeycloakEnvInput is one environment's share of SetupKeycloakWorkflow's
// work - the unit SetupKeycloakWorkflow fans out over.
type SetupKeycloakEnvInput struct {
	Config config.Config
	Env    string
}

// SetupKeycloakEnvWorkflow waits for one environment's Keycloak to become
// ready, provisions it, and writes out its exported SAML descriptor.
func SetupKeycloakEnvWorkflow(ctx workflow.Context, in SetupKeycloakEnvInput) error {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	logger := workflow.GetLogger(ctx)
	var a *activities.Activities

	cfg := in.Config
	baseURL := fmt.Sprintf("https://pmn-keycloak.%s.%s.%s", cfg.ProjectName, in.Env, cfg.TLD)
	logger.Info("Setting up Keycloak", "env", in.Env, "url", baseURL)

	if err := waitForKeycloakReady(ctx, a, baseURL); err != nil {
		return err
	}

	// Loaded via its own activity rather than read off cfg - see the doc
	// comment on activities.LoadKeycloakCredentials for why: LoadConfig
	// blanks this password out specifically so it doesn't appear in every
	// workflow's history, only this one's.
	creds, err := runActivity[activities.KeycloakCredentialsResult](ctx, a.LoadKeycloakCredentials)
	if err != nil {
		return fmt.Errorf("loading keycloak credentials: %w", err)
	}

	setup, err := runActivity[activities.RunKeycloakSetupResult](ctx, a.RunKeycloakSetup, activities.RunKeycloakSetupInput{
		BaseURL:       baseURL,
		AdminUsername: creds.AdminUsername,
		AdminPassword: creds.AdminPassword,
	})
	if err != nil {
		return err
	}

	appDir := filepath.Join(cfg.DestDir, "pmn-keycloak", "overlay", in.Env)
	files := []activities.FileWrite{
		{Path: filepath.Join(appDir, "sso.xml"), Content: setup.XML},
		{Path: filepath.Join(appDir, "sso.xml.b64"), Content: setup.B64},
	}

	if err := workflow.ExecuteActivity(ctx, a.WriteFiles, activities.WriteFilesInput{Files: files}).Get(ctx, nil); err != nil {
		return err
	}

	logger.Info("Wrote SAML descriptor", "dir", appDir)
	return nil
}

// waitForKeycloakReady polls Keycloak's health endpoint using a durable
// workflow timer between attempts. That durability is the whole point: the
// original Ruby version used a blocking `sleep`, so a process restart
// mid-wait lost all progress and started the 12-attempt countdown over.
// Here, workflow.Sleep's timer is recorded in Temporal's event history, so
// even a worker crash and restart resumes exactly where the wait left off.
func waitForKeycloakReady(ctx workflow.Context, a *activities.Activities, baseURL string) error {
	logger := workflow.GetLogger(ctx)

	for attempt := 1; ; attempt++ {
		ready, err := runActivity[activities.CheckKeycloakReadyResult](ctx, a.CheckKeycloakReady, activities.CheckKeycloakReadyInput{BaseURL: baseURL})
		if err != nil {
			return err
		}
		if ready.Ready {
			return nil
		}
		if attempt >= keycloakReadyMaxAttempts {
			return fmt.Errorf("keycloak did not become ready in time")
		}

		logger.Info("Keycloak not ready yet, waiting", "attempt", attempt, "delay", keycloakReadyPollDelay)
		if err := workflow.Sleep(ctx, keycloakReadyPollDelay); err != nil {
			return err
		}
	}
}
