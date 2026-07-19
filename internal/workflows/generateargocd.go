package workflows

import (
	"fmt"
	"path/filepath"

	"go.temporal.io/sdk/workflow"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/manifest"
)

// GenerateArgocdInput is the `setup-argo` command's workflow input.
type GenerateArgocdInput struct {
	DryRun bool
}

// GenerateArgocdResult summarizes what the workflow did.
type GenerateArgocdResult struct {
	AppsGenerated int
	EnvsGenerated int
	DryRun        bool
}

// GenerateArgocdWorkflow generates the single ArgoCD ApplicationSet that
// covers every app x environment combination, and (unless DryRun) writes it
// to Config.ClusterAppsDir.
//
// This used to write one Application manifest file per app x environment
// pair. That's no longer how the target GitOps repo owns these Applications:
// it moved to a single ApplicationSet with a matrix generator (one env list x
// one service list) so every environment shares one definition instead of
// duplicating it per file. If this workflow kept writing individual
// per-app-per-env files, they'd fight the ApplicationSet for ownership of
// the same Application names. Regenerating the whole ApplicationSet fresh
// every run - rather than trying to patch just the generator lists inside an
// existing file - matches how every other workflow in this tool already
// treats its output: Config plus what DiscoverApps finds is the single
// source of truth, and generated files are never hand-edited in place. The
// trade-off: any manual edit made directly to the ApplicationSet's
// boilerplate (e.g. flipping preserveResourcesOnDeletion off once a
// migration has settled) must be made here, in this template, too - it will
// otherwise be overwritten on the next run.
func GenerateArgocdWorkflow(ctx workflow.Context, in GenerateArgocdInput) (GenerateArgocdResult, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	logger := workflow.GetLogger(ctx)
	var a *activities.Activities

	// Config is loaded on whichever machine runs the worker, not wherever
	// this workflow was started from - see the doc comment on LoadConfig.
	cfgResult, err := runActivity[activities.LoadConfigResult](ctx, a.LoadConfig)
	if err != nil {
		return GenerateArgocdResult{}, fmt.Errorf("loading config: %w", err)
	}
	cfg := cfgResult.Config

	discovered, err := runActivity[activities.DiscoverAppsResult](ctx, a.DiscoverApps, activities.DiscoverAppsInput{Config: cfg})
	if err != nil {
		return GenerateArgocdResult{}, fmt.Errorf("discovering apps: %w", err)
	}
	logger.Info("Will generate ArgoCD ApplicationSet", "apps", len(discovered.Apps), "envs", len(cfg.Environments))

	// Unlike SyncWorkloads, this manifest is built fresh right here - none
	// of it came from parsing existing YAML, so there's no risk of an
	// activity-boundary JSON round trip corrupting integer fields (there
	// aren't any). Safe to render directly in workflow code.
	doc := argocdApplicationSetManifest(discovered.Apps, cfg)
	text, err := manifest.RenderYAML(doc)
	if err != nil {
		return GenerateArgocdResult{}, fmt.Errorf("rendering ApplicationSet: %w", err)
	}

	result := GenerateArgocdResult{
		AppsGenerated: len(discovered.Apps),
		EnvsGenerated: len(cfg.Environments),
		DryRun:        in.DryRun,
	}

	if in.DryRun {
		logger.Info("Dry run: skipping commit phase", "wouldWriteApps", result.AppsGenerated, "wouldWriteEnvs", result.EnvsGenerated)
		return result, nil
	}

	path := filepath.Join(cfg.ClusterAppsDir, fmt.Sprintf("%s-appset.yaml", cfg.ProjectName))
	logger.Info("Writing ArgoCD ApplicationSet", "path", path)
	if err := workflow.ExecuteActivity(ctx, a.WriteFiles, activities.WriteFilesInput{
		Files: []activities.FileWrite{{Path: path, Content: text}},
	}).Get(ctx, nil); err != nil {
		return GenerateArgocdResult{}, fmt.Errorf("writing ApplicationSet: %w", err)
	}

	return result, nil
}

// argocdApplicationSetManifest builds an ApplicationSet with a matrix
// generator over cfg.Environments x apps, expanding into one Application
// per combination named "<app>-<env>" - the same names the previous
// per-file Applications used, so the controller adopts them in place rather
// than recreating workloads.
//
// preserveResourcesOnDeletion and the lack of a resources-finalizer on the
// generated template are migration-safety settings: while the ApplicationSet
// is still settling in, deleting an entry from either list (or the whole
// ApplicationSet) orphans that Application's live resources instead of
// cascade-deleting them. Flip both once you're confident the migration has
// held - see the doc comment on GenerateArgocdWorkflow for what that means
// for a tool-regenerated file.
func argocdApplicationSetManifest(apps []string, cfg config.Config) map[string]any {
	envElements := make([]any, len(cfg.Environments))
	for i, env := range cfg.Environments {
		envElements[i] = map[string]any{"env": env}
	}

	appElements := make([]any, len(apps))
	for i, app := range apps {
		appElements[i] = map[string]any{"app": app}
	}

	return map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "ApplicationSet",
		"metadata": map[string]any{
			"name":      cfg.ProjectName,
			"namespace": "argocd",
		},
		"spec": map[string]any{
			"goTemplate":        true,
			"goTemplateOptions": []any{"missingkey=error"},
			"syncPolicy": map[string]any{
				"preserveResourcesOnDeletion": true,
			},
			"generators": []any{
				map[string]any{
					"matrix": map[string]any{
						"generators": []any{
							map[string]any{"list": map[string]any{"elements": envElements}},
							map[string]any{"list": map[string]any{"elements": appElements}},
						},
					},
				},
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"name": "{{.app}}-{{.env}}",
				},
				"spec": map[string]any{
					"project": "default",
					"source": map[string]any{
						"repoURL":        cfg.RepoURL,
						"targetRevision": "HEAD",
						"path":           "{{.app}}/overlay/{{.env}}",
					},
					"destination": map[string]any{
						"server":    "https://kubernetes.default.svc",
						"namespace": fmt.Sprintf("%s-{{.env}}", cfg.ProjectName),
					},
					"syncPolicy": map[string]any{
						"automated": map[string]any{
							"prune":    true,
							"selfHeal": true,
						},
						"syncOptions": []any{"CreateNamespace=true"},
						"retry": map[string]any{
							"limit": 5,
							"backoff": map[string]any{
								"duration":    "30s",
								"factor":      2,
								"maxDuration": "5m",
							},
						},
					},
				},
			},
		},
	}
}
