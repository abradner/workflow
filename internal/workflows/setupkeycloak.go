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

// SetupKeycloakWorkflow provisions the "neons" realm (OIDC + SAML clients,
// groups, seed users) in every target environment's Keycloak, then writes
// out its exported SAML descriptor.
//
// Unlike the other four orchestrators, essentially all of this tool's real
// work happens in what Ruby called the "commit phase" - so under DryRun it
// does nothing at all beyond logging the plan, exactly like the original.
//
// One environment's failure doesn't stop the others - matches Ruby's
// per-environment rescue in SetupKeycloak#commit_phase.
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

	for _, env := range cfg.Environments {
		baseURL := fmt.Sprintf("https://pmn-keycloak.%s.%s.%s", cfg.ProjectName, env, cfg.TLD)
		logger.Info("Setting up Keycloak", "env", env, "url", baseURL)

		if err := setupOneEnvironment(ctx, a, cfg, env, baseURL); err != nil {
			logger.Error("Failed to setup Keycloak", "env", env, "error", err.Error())
			continue
		}
		result.EnvironmentsSucceeded++
	}

	return result, nil
}

func setupOneEnvironment(ctx workflow.Context, a *activities.Activities, cfg config.Config, env, baseURL string) error {
	logger := workflow.GetLogger(ctx)

	logger.Info("Waiting for Keycloak to be ready", "url", baseURL)
	if err := waitForKeycloakReady(ctx, a, baseURL); err != nil {
		return err
	}

	setup, err := runActivity[activities.RunKeycloakSetupResult](ctx, a.RunKeycloakSetup, activities.RunKeycloakSetupInput{
		BaseURL:       baseURL,
		AdminUsername: cfg.KeycloakAdmin,
		AdminPassword: cfg.KeycloakAdminPassword,
	})
	if err != nil {
		return err
	}

	appDir := filepath.Join(cfg.DestDir, "pmn-keycloak", "overlay", env)
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
