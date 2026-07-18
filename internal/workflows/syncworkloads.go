package workflows

import (
	"errors"
	"fmt"

	"go.temporal.io/sdk/workflow"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
)

// SyncWorkloadsInput is the `sync` command's workflow input.
type SyncWorkloadsInput struct {
	DryRun bool
}

// SyncWorkloadsResult summarizes what the workflow did.
type SyncWorkloadsResult struct {
	AppsProcessed int
	FilesWritten  int
	DryRun        bool
}

// SyncWorkloadsWorkflow discovers every app under Config.SourceDir, then
// fans out one SyncAppWorkflow child per app to extract, transform, and
// (unless DryRun) commit its manifests.
//
// Each app runs as its own child workflow rather than one big inline loop.
// That buys two things: apps build concurrently instead of one after
// another, and - the thing that actually motivated this change, see the
// "Decomposing the monolith" section of docs/GO_NOTES.md - each child's own
// Temporal event history only ever holds that one app's rendered files. The
// previous version accumulated every app's files into a single `allFiles`
// slice and passed the whole thing to one final WriteFiles call; for a
// large enough source tree that trips Temporal's default 2MB payload/4MB
// gRPC message limits before anything gets written. Per-app children keep
// every activity payload bounded to one app's own size.
func SyncWorkloadsWorkflow(ctx workflow.Context, in SyncWorkloadsInput) (SyncWorkloadsResult, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	logger := workflow.GetLogger(ctx)
	var a *activities.Activities // nil receiver - see ExecuteActivity's docs on calling activity methods

	// Config is loaded on whichever machine runs the worker, not wherever
	// this workflow was started from - see the doc comment on LoadConfig.
	cfgResult, err := runActivity[activities.LoadConfigResult](ctx, a.LoadConfig)
	if err != nil {
		return SyncWorkloadsResult{}, fmt.Errorf("loading config: %w", err)
	}
	cfg := cfgResult.Config

	discovered, err := runActivity[activities.DiscoverAppsResult](ctx, a.DiscoverApps, activities.DiscoverAppsInput{Config: cfg})
	if err != nil {
		return SyncWorkloadsResult{}, fmt.Errorf("discovering apps: %w", err)
	}
	logger.Info("Discovered applications", "count", len(discovered.Apps))

	// Fan out: start every app's child workflow before waiting on any of
	// them, so Temporal schedules them concurrently rather than one at a
	// time. ExecuteChildWorkflow returns immediately with a future; nothing
	// actually runs until we Get() it (or the parent workflow completes).
	futures := make([]workflow.ChildWorkflowFuture, len(discovered.Apps))
	for i, app := range discovered.Apps {
		futures[i] = workflow.ExecuteChildWorkflow(ctx, SyncAppWorkflow, SyncAppInput{
			Config:  cfg,
			AppName: app,
			DryRun:  in.DryRun,
		})
	}

	// Fan in: wait for every child and collect every failure instead of
	// returning on the first one, so one broken app can't hide whether the
	// rest succeeded.
	var filesWritten int
	var errs error
	for i, f := range futures {
		var appResult SyncAppResult
		if err := f.Get(ctx, &appResult); err != nil {
			errs = errors.Join(errs, fmt.Errorf("app %s: %w", discovered.Apps[i], err))
			continue
		}
		filesWritten += appResult.FilesWritten
	}
	if errs != nil {
		return SyncWorkloadsResult{}, errs
	}

	if in.DryRun {
		logger.Info("Dry run: no files written", "wouldWriteFiles", filesWritten)
	} else {
		logger.Info("Committed every app", "apps", len(discovered.Apps), "files", filesWritten, "destDir", cfg.DestDir)
	}

	return SyncWorkloadsResult{
		AppsProcessed: len(discovered.Apps),
		FilesWritten:  filesWritten,
		DryRun:        in.DryRun,
	}, nil
}

// SyncAppInput is one app's share of SyncWorkloadsWorkflow's work - the unit
// SyncWorkloadsWorkflow fans out over.
type SyncAppInput struct {
	Config  config.Config
	AppName string
	DryRun  bool
}

// SyncAppResult summarizes what SyncAppWorkflow did for one app.
type SyncAppResult struct {
	FilesWritten int
}

// SyncAppWorkflow builds one app's manifests and, unless DryRun, writes
// them. Keeping build and write together and scoped to a single app is what
// keeps this child's own Temporal history bounded to one app's file sizes
// instead of the whole source tree's - see SyncWorkloadsWorkflow's doc
// comment.
func SyncAppWorkflow(ctx workflow.Context, in SyncAppInput) (SyncAppResult, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	logger := workflow.GetLogger(ctx)
	var a *activities.Activities

	logger.Info("Extracting and transforming workspace", "app", in.AppName)
	built, err := runActivity[activities.BuildAppFilesResult](ctx, a.BuildAppFiles, activities.BuildAppFilesInput{
		Config:  in.Config,
		AppName: in.AppName,
	})
	if err != nil {
		return SyncAppResult{}, fmt.Errorf("building files: %w", err)
	}

	if in.DryRun {
		logger.Info("Dry run: skipping commit", "wouldWriteFiles", len(built.Files))
		return SyncAppResult{FilesWritten: len(built.Files)}, nil
	}

	if err := workflow.ExecuteActivity(ctx, a.WriteFiles, activities.WriteFilesInput{Files: built.Files}).Get(ctx, nil); err != nil {
		return SyncAppResult{}, fmt.Errorf("writing files: %w", err)
	}
	logger.Info("Committed app", "app", in.AppName, "files", len(built.Files))

	return SyncAppResult{FilesWritten: len(built.Files)}, nil
}
