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
	ManifestsGenerated int
	DryRun             bool
}

// GenerateArgocdWorkflow generates an ArgoCD Application manifest for every
// app x environment combination and (unless DryRun) writes them to
// Config.ClusterAppsDir.
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
	logger.Info("Will generate ArgoCD Application manifests", "apps", len(discovered.Apps), "envs", len(cfg.Environments))

	// Unlike SyncWorkloads, this manifest is built fresh right here - none
	// of it came from parsing existing YAML, so there's no risk of an
	// activity-boundary JSON round trip corrupting integer fields (there
	// aren't any). Safe to render directly in workflow code.
	var files []activities.FileWrite
	for _, app := range discovered.Apps {
		for _, env := range cfg.Environments {
			doc := argocdApplicationManifest(app, env, cfg)

			text, err := manifest.RenderYAML(doc)
			if err != nil {
				return GenerateArgocdResult{}, fmt.Errorf("rendering manifest for %s/%s: %w", app, env, err)
			}

			files = append(files, activities.FileWrite{
				Path:    filepath.Join(cfg.ClusterAppsDir, fmt.Sprintf("%s-%s.yaml", app, env)),
				Content: text,
			})
		}
	}

	result := GenerateArgocdResult{ManifestsGenerated: len(files), DryRun: in.DryRun}

	if in.DryRun {
		logger.Info("Dry run: skipping commit phase", "wouldWriteFiles", len(files))
		return result, nil
	}

	logger.Info("Writing ArgoCD App manifests", "count", len(files))
	if err := workflow.ExecuteActivity(ctx, a.WriteFiles, activities.WriteFilesInput{Files: files}).Get(ctx, nil); err != nil {
		return GenerateArgocdResult{}, fmt.Errorf("writing files: %w", err)
	}

	return result, nil
}

func argocdApplicationManifest(appName, env string, cfg config.Config) map[string]any {
	return map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":       fmt.Sprintf("%s-%s", appName, env),
			"namespace":  "argocd",
			"finalizers": []any{"resources-finalizer.argocd.argoproj.io"},
		},
		"spec": map[string]any{
			"project": "default",
			"source": map[string]any{
				"repoURL":        cfg.RepoURL,
				"targetRevision": "main",
				"path":           fmt.Sprintf("%s-workloads/%s/overlay/%s", cfg.ProjectName, appName, env),
			},
			"destination": map[string]any{
				"server":    "https://kubernetes.default.svc",
				"namespace": fmt.Sprintf("%s-%s", cfg.ProjectName, env),
			},
			"syncPolicy": map[string]any{
				"automated": map[string]any{
					"prune":    true,
					"selfHeal": true,
				},
				"syncOptions": []any{"CreateNamespace=true"},
			},
		},
	}
}
