// Package activities holds every Temporal activity this workflow engine
// uses - the boundary where real I/O (disk, AWS, 1Password, Keycloak)
// happens. Everything upstream of this package (transformers, domain,
// manifest, the pure parts of services) is plain deterministic Go that
// doesn't know Temporal exists.
//
// Activities are methods on *Activities so the real dependencies
// (filesystem, AWS, 1Password, Keycloak) can be swapped for fakes in tests
// without a Temporal server in the loop.
package activities

import (
	"context"
	"fmt"
	"path/filepath"

	"go.temporal.io/sdk/activity"

	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/domain"
	"github.com/abradner/workflow/internal/manifest"
	"github.com/abradner/workflow/internal/serviceclients/keycloak"
	"github.com/abradner/workflow/internal/serviceclients/op"
	"github.com/abradner/workflow/internal/services/awssecrets"
	"github.com/abradner/workflow/internal/services/discoversamlcreds"
	"github.com/abradner/workflow/internal/services/filesystem"
	"github.com/abradner/workflow/internal/services/keycloaksetup"
	"github.com/abradner/workflow/internal/services/onepassword"
	"github.com/abradner/workflow/internal/services/workspaceextractor"
	"github.com/abradner/workflow/internal/transformers"
)

// Activities bundles the real, effectful dependencies every activity method
// needs. Construct one with New for production use, or build the struct
// literal directly in tests with fakes for AWSSecrets/OnePassword/NewKeycloak.
type Activities struct {
	FS          *filesystem.Service
	AWSSecrets  *awssecrets.Service
	OnePassword *op.Client
	NewKeycloak func(baseURL string) *keycloak.Client
}

// New builds an Activities using real AWS credentials, a real `op` CLI
// wrapper, and real Keycloak HTTP clients.
func New(ctx context.Context) (*Activities, error) {
	aws, err := awssecrets.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("building AWS secrets client: %w", err)
	}

	return &Activities{
		FS:          filesystem.New(),
		AWSSecrets:  aws,
		OnePassword: op.New(),
		NewKeycloak: keycloak.New,
	}, nil
}

// FileWrite is a fully-rendered file ready to land on disk: an absolute
// destination path and its final text content.
type FileWrite struct {
	Path    string
	Content string
}

// --- Discovery ---------------------------------------------------------

type DiscoverAppsInput struct {
	Config config.Config
}

type DiscoverAppsResult struct {
	Apps []string
}

// DiscoverApps scans SourceDir for app directories matching AppPattern.
func (a *Activities) DiscoverApps(_ context.Context, in DiscoverAppsInput) (DiscoverAppsResult, error) {
	dirs, err := a.FS.ListDirectories(in.Config.SourceDir, in.Config.AppPattern)
	if err != nil {
		return DiscoverAppsResult{}, err
	}

	apps := make([]string, len(dirs))
	for i, d := range dirs {
		apps[i] = a.FS.BaseFilename(d)
	}
	return DiscoverAppsResult{Apps: apps}, nil
}

// --- SyncWorkloads -------------------------------------------------------

type BuildAppFilesInput struct {
	Config  config.Config
	AppName string
}

type BuildAppFilesResult struct {
	Files []FileWrite
}

// BuildAppFiles extracts an app's manifests, runs the transformer pipeline,
// and renders every result to its final YAML/text content.
//
// Extraction, transformation, and rendering are bundled into one activity
// deliberately: the transformer pipeline is pure and could run directly in
// workflow code, but the manifests it operates on are parsed YAML
// (map[string]any) that may contain integers (e.g. a port number). Once
// that crosses the workflow/activity boundary, Temporal's default JSON data
// converter loses the int/float distinction (JSON doesn't have separate
// types, so decoding into `any` always yields float64) - which can silently
// turn `port: 80` into `port: 80.0` in the rendered YAML. Keeping
// extract+transform+render together means only the final strings ever
// cross the boundary, sidestepping the problem entirely.
func (a *Activities) BuildAppFiles(_ context.Context, in BuildAppFilesInput) (BuildAppFilesResult, error) {
	cfg := in.Config

	extractor := workspaceextractor.New(cfg.SourceDir, cfg.SourceEnv, cfg.Environments, a.FS)
	ws, err := extractor.Extract(in.AppName)
	if err != nil {
		return BuildAppFilesResult{}, fmt.Errorf("extracting %s: %w", in.AppName, err)
	}

	// Transformer ordering is significant: EnvironmentGenerator must run
	// first (it clones the source overlay into every target env); the rest
	// operate independently on the fully expanded workspace.
	ws = transformers.EnvironmentGenerator(ws)
	ws = transformers.LegacyModernizer{
		ExternalSecretsAPIVersion: cfg.ExternalSecretsAPIVersion,
		ProjectName:               cfg.ProjectName,
		TLD:                       cfg.TLD,
	}.Call(ws)
	ws = transformers.PullSecretInjector{
		RegistryHostname:          cfg.RegistryHostname,
		Registry1PItemID:          cfg.Registry1PItemID,
		ExternalSecretsAPIVersion: cfg.ExternalSecretsAPIVersion,
		ProjectName:               cfg.ProjectName,
	}.Call(ws)
	ws = transformers.ServiceAbstractionLinker{
		ProjectName: cfg.ProjectName,
		TLD:         cfg.TLD,
	}.Call(ws)

	files := make([]FileWrite, 0, len(ws.Manifests))
	for _, path := range ws.SortedPaths() {
		text, err := renderManifest(path, ws.Manifests[path])
		if err != nil {
			return BuildAppFilesResult{}, fmt.Errorf("rendering %s/%s: %w", in.AppName, path, err)
		}
		files = append(files, FileWrite{
			Path:    filepath.Join(cfg.DestDir, ws.AppName, path),
			Content: text,
		})
	}

	return BuildAppFilesResult{Files: files}, nil
}

func renderManifest(path string, content any) (string, error) {
	if manifest.IsYAMLPath(path) {
		return manifest.RenderYAML(content)
	}
	text, _ := content.(string)
	return text, nil
}

// --- Writing --------------------------------------------------------------

type WriteFilesInput struct {
	Files []FileWrite
}

// WriteFiles commits a batch of already-rendered files to disk, creating
// parent directories as needed. Used by every workflow's commit phase.
func (a *Activities) WriteFiles(_ context.Context, in WriteFilesInput) error {
	for _, f := range in.Files {
		if err := a.FS.CreateDirectory(filepath.Dir(f.Path)); err != nil {
			return fmt.Errorf("creating directory for %s: %w", f.Path, err)
		}
		if err := a.FS.WriteFile(f.Path, f.Content); err != nil {
			return fmt.Errorf("writing %s: %w", f.Path, err)
		}
	}
	return nil
}

// --- Sync1Password ---------------------------------------------------------

type ExtractAWSSecretsInput struct {
	Env string
}

type ExtractAWSSecretsResult struct {
	Secrets []domain.ExtractedSecret
}

// ExtractAWSSecrets lists and fetches every AWS Secrets Manager secret for Env.
func (a *Activities) ExtractAWSSecrets(ctx context.Context, in ExtractAWSSecretsInput) (ExtractAWSSecretsResult, error) {
	secrets, err := a.AWSSecrets.ExtractSecrets(ctx, in.Env)
	if err != nil {
		return ExtractAWSSecretsResult{}, err
	}
	return ExtractAWSSecretsResult{Secrets: secrets}, nil
}

type FetchSamlCredentialsInput struct {
	RealmName string
	BaseURL   string
}

type FetchSamlCredentialsResult struct {
	Credentials *domain.SamlCredentials
}

// FetchSamlCredentials fetches SAML/OIDC material from a Keycloak realm.
// Credentials is nil (not an error) if the realm couldn't be reached.
func (a *Activities) FetchSamlCredentials(ctx context.Context, in FetchSamlCredentialsInput) (FetchSamlCredentialsResult, error) {
	svc := discoversamlcreds.New(
		func(baseURL string) discoversamlcreds.KeycloakClient { return a.NewKeycloak(baseURL) },
		activity.GetLogger(ctx),
	)
	return FetchSamlCredentialsResult{Credentials: svc.FetchFor(ctx, in.RealmName, in.BaseURL)}, nil
}

type IngestVaultItemInput struct {
	ProjectName string
	Env         string
	Secrets     []domain.ExtractedSecret
}

// IngestVaultItem creates the "k8s-<project>-<env>" Secure Note in 1Password.
func (a *Activities) IngestVaultItem(ctx context.Context, in IngestVaultItemInput) error {
	svc := onepassword.New(in.ProjectName, a.OnePassword)
	_, err := svc.IngestVaultItem(ctx, in.Env, in.Secrets)
	return err
}

// --- RenderTalos ------------------------------------------------------------

type ReadOnePasswordNoteInput struct {
	ItemID string
}

type ReadOnePasswordNoteResult struct {
	Content string
}

// ReadOnePasswordNote reads a Secure Note's notesPlain field.
func (a *Activities) ReadOnePasswordNote(ctx context.Context, in ReadOnePasswordNoteInput) (ReadOnePasswordNoteResult, error) {
	content, err := a.OnePassword.ReadNote(ctx, in.ItemID)
	if err != nil {
		return ReadOnePasswordNoteResult{}, err
	}
	return ReadOnePasswordNoteResult{Content: content}, nil
}

type ReadTemplateFilesInput struct {
	TemplateDir string
}

type ReadTemplateFilesResult struct {
	// Paths[i] and Contents[i] describe the same file - parallel slices
	// rather than a map, so the order Discovery found them in survives
	// the trip back into workflow code untouched.
	Paths    []string
	Contents []string
}

// ReadTemplateFiles finds every *.template.yaml under TemplateDir and reads
// its raw content.
func (a *Activities) ReadTemplateFiles(_ context.Context, in ReadTemplateFilesInput) (ReadTemplateFilesResult, error) {
	paths, err := a.FS.Glob(filepath.Join(in.TemplateDir, "*.template.yaml"))
	if err != nil {
		return ReadTemplateFilesResult{}, err
	}

	contents := make([]string, len(paths))
	for i, path := range paths {
		content, err := a.FS.ReadFile(path)
		if err != nil {
			return ReadTemplateFilesResult{}, fmt.Errorf("reading %s: %w", path, err)
		}
		contents[i] = content
	}

	return ReadTemplateFilesResult{Paths: paths, Contents: contents}, nil
}

// --- SetupKeycloak ----------------------------------------------------------

type CheckKeycloakReadyInput struct {
	BaseURL string
}

type CheckKeycloakReadyResult struct {
	Ready bool
}

// CheckKeycloakReady checks Keycloak's health endpoint once. The workflow
// layer calls this in a poll loop backed by a durable timer.
func (a *Activities) CheckKeycloakReady(ctx context.Context, in CheckKeycloakReadyInput) (CheckKeycloakReadyResult, error) {
	return CheckKeycloakReadyResult{Ready: a.NewKeycloak(in.BaseURL).Ready(ctx)}, nil
}

type RunKeycloakSetupInput struct {
	BaseURL       string
	AdminUsername string
	AdminPassword string
}

type RunKeycloakSetupResult struct {
	XML string
	B64 string
}

// RunKeycloakSetup provisions the realm, clients, groups, and seed users,
// and returns the exported SAML descriptor.
func (a *Activities) RunKeycloakSetup(ctx context.Context, in RunKeycloakSetupInput) (RunKeycloakSetupResult, error) {
	client := a.NewKeycloak(in.BaseURL)
	svc := keycloaksetup.New(client, activity.GetLogger(ctx))

	descriptors, err := svc.Setup(ctx, in.AdminUsername, in.AdminPassword)
	if err != nil {
		return RunKeycloakSetupResult{}, err
	}
	return RunKeycloakSetupResult{XML: descriptors.XML, B64: descriptors.B64}, nil
}
