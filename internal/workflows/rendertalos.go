package workflows

import (
	"fmt"
	"path/filepath"
	"strings"

	"go.temporal.io/sdk/workflow"
	"gopkg.in/yaml.v3"

	"github.com/abradner/workflow/internal/activities"
	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/services/templaterendering"
)

// RenderTalosInput is the `render-talos` command's workflow input.
type RenderTalosInput struct {
	Config config.Config
	DryRun bool
}

// RenderTalosResult summarizes what the workflow did.
type RenderTalosResult struct {
	SecretKeysLoaded  int
	TemplatesRendered int
	DryRun            bool
}

// RenderTalosWorkflow reads a 1Password Secure Note containing the Talos
// secrets.yaml content, then hydrates every *.template.yaml file in
// Config.TalosTemplateDir by substituting "{{ dotted.key }}" placeholders.
// Unlike the other orchestrators, this one never touches app discovery or
// the transformer pipeline - it's its own linear extract/render/write flow,
// same as the original.
func RenderTalosWorkflow(ctx workflow.Context, in RenderTalosInput) (RenderTalosResult, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	logger := workflow.GetLogger(ctx)
	var a *activities.Activities
	cfg := in.Config

	logger.Info("Reading Secure Note from 1Password", "itemID", cfg.TalosItemID)
	note, err := runActivity[activities.ReadOnePasswordNoteResult](ctx, a.ReadOnePasswordNote, activities.ReadOnePasswordNoteInput{ItemID: cfg.TalosItemID})
	if err != nil {
		return RenderTalosResult{}, fmt.Errorf("reading 1Password note: %w", err)
	}

	// Parsing the note and rendering templates is pure string/YAML
	// processing with no I/O, so it runs directly here rather than in an
	// activity.
	var secretsHash map[string]any
	if err := yaml.Unmarshal([]byte(note.Content), &secretsHash); err != nil {
		return RenderTalosResult{}, fmt.Errorf("parsing Secure Note YAML: %w", err)
	}
	flatSecrets := templaterendering.FlattenHash(secretsHash)
	logger.Info("Loaded secret keys", "count", len(flatSecrets))

	templates, err := runActivity[activities.ReadTemplateFilesResult](ctx, a.ReadTemplateFiles, activities.ReadTemplateFilesInput{TemplateDir: cfg.TalosTemplateDir})
	if err != nil {
		return RenderTalosResult{}, fmt.Errorf("reading template files: %w", err)
	}
	logger.Info("Found template files", "count", len(templates.Paths), "dir", cfg.TalosTemplateDir)

	var files []activities.FileWrite
	var allMissing []string

	for i, path := range templates.Paths {
		content := templates.Contents[i]

		missing := templaterendering.MissingKeys(content, flatSecrets)
		if len(missing) > 0 {
			logger.Error("Missing keys", "file", filepath.Base(path), "keys", strings.Join(missing, ", "))
			allMissing = append(allMissing, missing...)
			continue
		}

		rendered, err := templaterendering.Render(content, flatSecrets)
		if err != nil {
			return RenderTalosResult{}, err
		}

		outputName := strings.TrimSuffix(filepath.Base(path), ".template.yaml") + ".yaml"
		files = append(files, activities.FileWrite{
			Path:    filepath.Join(cfg.TalosTemplateDir, outputName),
			Content: rendered,
		})
	}

	if unresolved := uniqueStrings(allMissing); len(unresolved) > 0 {
		return RenderTalosResult{}, fmt.Errorf(
			"cannot hydrate: %d unresolved placeholder(s) - add them to the Secure Note or fix the templates",
			len(unresolved))
	}
	logger.Info("All placeholders validated")

	result := RenderTalosResult{SecretKeysLoaded: len(flatSecrets), TemplatesRendered: len(files), DryRun: in.DryRun}

	if in.DryRun {
		logger.Info("Dry run: skipping commit phase", "wouldWriteFiles", len(files))
		return result, nil
	}

	if err := workflow.ExecuteActivity(ctx, a.WriteFiles, activities.WriteFilesInput{Files: files}).Get(ctx, nil); err != nil {
		return RenderTalosResult{}, fmt.Errorf("writing rendered files: %w", err)
	}

	return result, nil
}
