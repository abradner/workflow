package workflows

import (
	"fmt"

	"go.temporal.io/sdk/workflow"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
)

// SyncWorkloadsInput is the `sync` command's workflow input.
type SyncWorkloadsInput struct {
	Config config.Config
	DryRun bool
}

// SyncWorkloadsResult summarizes what the workflow did.
type SyncWorkloadsResult struct {
	AppsProcessed int
	FilesWritten  int
	DryRun        bool
}

// SyncWorkloadsWorkflow discovers every app under Config.SourceDir, extracts
// and transforms its manifests for every target environment, and (unless
// DryRun) writes the result to Config.DestDir.
func SyncWorkloadsWorkflow(ctx workflow.Context, in SyncWorkloadsInput) (SyncWorkloadsResult, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	logger := workflow.GetLogger(ctx)
	var a *activities.Activities // nil receiver - see ExecuteActivity's docs on calling activity methods

	discovered, err := runActivity[activities.DiscoverAppsResult](ctx, a.DiscoverApps, activities.DiscoverAppsInput{Config: in.Config})
	if err != nil {
		return SyncWorkloadsResult{}, fmt.Errorf("discovering apps: %w", err)
	}
	logger.Info("Discovered applications", "count", len(discovered.Apps))

	var allFiles []activities.FileWrite
	for _, app := range discovered.Apps {
		logger.Info("Extracting and transforming workspace", "app", app)

		built, err := runActivity[activities.BuildAppFilesResult](ctx, a.BuildAppFiles, activities.BuildAppFilesInput{
			Config:  in.Config,
			AppName: app,
		})
		if err != nil {
			return SyncWorkloadsResult{}, fmt.Errorf("building files for %s: %w", app, err)
		}
		allFiles = append(allFiles, built.Files...)
	}

	result := SyncWorkloadsResult{AppsProcessed: len(discovered.Apps), FilesWritten: len(allFiles), DryRun: in.DryRun}

	if in.DryRun {
		logger.Info("Dry run: skipping commit phase", "wouldWriteFiles", len(allFiles))
		return result, nil
	}

	logger.Info("Committing planned workspaces", "files", len(allFiles), "destDir", in.Config.DestDir)
	if err := workflow.ExecuteActivity(ctx, a.WriteFiles, activities.WriteFilesInput{Files: allFiles}).Get(ctx, nil); err != nil {
		return SyncWorkloadsResult{}, fmt.Errorf("writing files: %w", err)
	}

	return result, nil
}
