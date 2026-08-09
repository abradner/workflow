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
	"strings"

	"go.temporal.io/sdk/activity"
	"gopkg.in/yaml.v3"

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
	"github.com/abradner/workflow/internal/services/templaterendering"
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

// --- Config --------------------------------------------------------------

type LoadConfigResult struct {
	Config config.Config
}

// LoadConfig loads Config from the worker process's own environment (.env +
// env vars).
//
// This has to be an activity - called by every workflow as its first step -
// rather than a value the CLI client loads and passes in as workflow input.
// A Temporal client and the worker that executes the workflow can be
// different machines with different filesystems (that's the whole point of
// --temporal=<host:port> mode: docker-compose runs the worker in a
// container with its own mounted paths, while the CLI command that starts
// the workflow runs on the host). Config carries filesystem paths
// (SourceDir, DestDir, ...) that are only meaningful relative to whichever
// process actually performs the I/O - the worker - so that's the only place
// they can correctly be read from.
func (a *Activities) LoadConfig(_ context.Context) (LoadConfigResult, error) {
	cfg, err := config.Load()
	if err != nil {
		return LoadConfigResult{}, err
	}

	// Blanked out deliberately: LoadConfig's result is recorded in every
	// workflow's durable Temporal event history (visible via the Web
	// UI/API/DB in external mode). The Keycloak admin password is only
	// ever needed inside RunKeycloakSetup, which loads it directly via its
	// own config.Load() call rather than receiving it as input - see that
	// activity's doc comment for why even a dedicated credentials activity
	// isn't good enough (its result would still round-trip through
	// workflow code on the way to RunKeycloakSetup's own input).
	cfg.KeycloakAdmin = ""
	cfg.KeycloakAdminPassword = ""

	return LoadConfigResult{Config: *cfg}, nil
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
	DryRun  bool
}

type BuildAppFilesResult struct {
	FilesWritten int
}

// BuildAppFiles extracts an app's manifests, runs the transformer pipeline,
// renders every result to its final YAML/text content, and, unless DryRun,
// writes it to disk.
//
// Extraction, transformation, rendering, and writing are all bundled into
// one activity deliberately, for two independent reasons:
//
//  1. The transformer pipeline is pure and could run directly in workflow
//     code, but the manifests it operates on are parsed YAML
//     (map[string]any) that may contain integers (e.g. a port number). Once
//     that crosses the workflow/activity boundary, Temporal's default JSON
//     data converter loses the int/float distinction (decoding into `any`
//     always yields float64), which can silently turn `port: 80` into
//     `port: 80.0` in the rendered YAML.
//  2. Rendered manifest content can be large enough, for a single app, to
//     risk Temporal's default 2MB payload/4MB gRPC message limits - and
//     that risk exists whether the content crosses the boundary once (as
//     this activity's result) or twice (also as a separate WriteFiles
//     call's input, the way earlier versions of this workflow did it).
//     Writing here too means the rendered content never has to leave this
//     activity at all - only a final file count crosses back into
//     SyncAppWorkflow.
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

	if in.DryRun {
		return BuildAppFilesResult{FilesWritten: len(files)}, nil
	}

	for _, f := range files {
		if err := a.FS.CreateDirectory(filepath.Dir(f.Path)); err != nil {
			return BuildAppFilesResult{}, fmt.Errorf("creating directory for %s: %w", f.Path, err)
		}
		if err := a.FS.WriteFile(f.Path, f.Content); err != nil {
			return BuildAppFilesResult{}, fmt.Errorf("writing %s: %w", f.Path, err)
		}
	}

	return BuildAppFilesResult{FilesWritten: len(files)}, nil
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
// parent directories as needed. Used where the rendered content is small
// and non-sensitive enough that crossing the workflow boundary as its own
// activity input isn't a concern (GenerateArgocd's manifests,
// SetupKeycloak's SSO descriptor) - see BuildAppFiles and
// RenderTalosTemplates for the two cases where it wasn't and writing was
// folded into the activity that produces the content instead.
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

type SyncEnvSecretsInput struct {
	ProjectName string
	VaultName   string

	// ExactSecretNames are fetched by name in addition to the environment
	// filter - see awssecrets.ExtractExact. Names only; no values cross this
	// boundary.
	ExactSecretNames []string

	// Prune deletes fields this run did not write. Off unless `prune-1p` was
	// the command; never inferred from anything else.
	Prune       bool
	SourceEnv   string
	TargetEnv   string
	KCPublicKey string // "" means no fresh Keycloak key to inject
	DryRun      bool
}

type SyncEnvSecretsResult struct {
	SecretsExtracted int

	// ItemCreated distinguishes a first run for an environment from an update
	// of an existing item.
	ItemCreated bool

	// StaleFields counts fields the vault holds that this run did not write.
	// A count, deliberately: field labels are close enough to the secrets
	// themselves, and everything returned from an activity is recorded in
	// Temporal's durable, readable event history.
	StaleFields int
}

// SyncEnvSecrets extracts every AWS secret for SourceEnv, remaps it onto
// TargetEnv (injecting KCPublicKey into any mapped payload that carries an
// "mp.jwt.verify.publickey" field), and, unless DryRun, provisions
// TargetEnv's 1Password Secure Note from the result.
//
// Extraction, mapping, and ingestion are bundled into one activity
// deliberately: the actual secret VALUES must never appear as an activity
// result or workflow/child-workflow input, because Temporal records both,
// byte-for-byte, in its durable event history (visible via the Web
// UI/API/DB in external mode). An earlier version of this workflow
// extracted once in the parent workflow and passed the plaintext secrets
// down into every per-environment child - correct in spirit (share one
// extraction across environments) but wrong in practice, since that put
// real secret values in both the parent's and every child's history.
// Keeping the whole extract-map-ingest pipeline inside one activity call
// per environment means only a final secret *count* ever crosses back into
// workflow code, at the cost of one extra (idempotent, read-only) AWS API
// round trip per target environment instead of one shared round trip.
//
// This activity runs with retries disabled (see nonRetryingActivityOptions
// at the call site) for the same reason IngestVaultItem always did: the
// `op item create` call it ends with isn't safe to retry. That also means
// a transient AWS-side failure during extraction doesn't get Temporal's
// usual automatic retry either - an accepted trade for keeping the secrets
// out of Temporal's history. Rerun the command if that happens.
func (a *Activities) SyncEnvSecrets(ctx context.Context, in SyncEnvSecretsInput) (SyncEnvSecretsResult, error) {
	// Checked before any AWS call: failing here costs nothing, whereas failing
	// after extraction has already pulled every secret for the environment.
	// Not enforced in config.Load because the other four workflows have no
	// vault to name - see the OPVaultName field comment.
	if !in.DryRun && in.VaultName == "" {
		return SyncEnvSecretsResult{}, fmt.Errorf("OP_VAULT_NAME is not set: sync-1p needs a target vault, or items land in the personal vault")
	}

	secrets, err := a.AWSSecrets.ExtractSecrets(ctx, in.SourceEnv)
	if err != nil {
		return SyncEnvSecretsResult{}, fmt.Errorf("extracting AWS secrets: %w", err)
	}

	// Secrets outside the environment naming convention, fetched by name.
	// Merged with the environment extract, exact entries winning a collision:
	// naming one explicitly is a deliberate act, and should beat whatever the
	// filter happened to match.
	if len(in.ExactSecretNames) > 0 {
		// WithLogger returns a copy: Activities is shared across concurrent
		// activity executions, so attaching a logger to the shared service
		// would race across the per-environment fan-out.
		exact, err := a.AWSSecrets.WithLogger(activity.GetLogger(ctx)).ExtractExact(ctx, in.ExactSecretNames)
		if err != nil {
			return SyncEnvSecretsResult{}, fmt.Errorf("extracting named AWS secrets: %w", err)
		}
		secrets = mergeSecrets(secrets, exact)
	}

	logger := activity.GetLogger(ctx)

	// Key injection first, on the raw payloads: the injector rewrites a JSON
	// secret in place, and doing it before mapping means the mapper sees final
	// values and never has to reach back into fields it already wrote.
	injected := transformers.OnePasswordSamlKeyInjector{
		KCPublicKey: in.KCPublicKey,
		Logger:      logger,
	}.Call(secrets)

	if in.DryRun {
		return SyncEnvSecretsResult{SecretsExtracted: len(secrets)}, nil
	}

	svc := onepassword.New(in.ProjectName, in.VaultName, a.OnePassword)

	// Read before writing. This is what makes the run an upsert: the vault's
	// own field IDs come back with the item and are preserved through
	// UpsertField, so a field stays the same field across runs.
	item, err := svc.Load(ctx, in.TargetEnv)
	if err != nil {
		return SyncEnvSecretsResult{}, err
	}

	transformers.OnePasswordItemMapper{
		SourceEnv:  in.SourceEnv,
		TargetEnv:  in.TargetEnv,
		Logger:     logger,
		NewFieldID: onepassword.NewFieldID,
	}.Call(item, injected)

	committed, err := svc.Commit(ctx, item, onepassword.CommitOptions{Prune: in.Prune})
	if err != nil {
		return SyncEnvSecretsResult{}, err
	}

	// Warned about, never acted on. A stale field is often something a human
	// put there deliberately, so removing it is an explicit, separate act.
	//
	// Counted rather than named because the count travels back in
	// SyncEnvSecretsResult, and activity results ARE recorded in Temporal's
	// durable event history. (This log line itself is not - worker logs and
	// event history are different sinks - but the value it prints is the same
	// one that crosses the boundary, so the reasoning holds either way.)
	if committed.StaleFields > 0 {
		logger.Warn("Vault item has fields this run did not write; they are preserved",
			"env", in.TargetEnv, "count", committed.StaleFields)
	}
	if len(committed.UnknownKeys) > 0 {
		logger.Warn("1Password returned item keys this tool does not model; written back untouched",
			"env", in.TargetEnv, "keys", committed.UnknownKeys)
	}

	return SyncEnvSecretsResult{
		SecretsExtracted: len(secrets),
		ItemCreated:      committed.Created,
		StaleFields:      committed.StaleFields,
	}, nil
}

// --- RenderTalos ------------------------------------------------------------

type RenderTalosTemplatesInput struct {
	ItemID      string
	TemplateDir string
	DryRun      bool
}

type RenderTalosTemplatesResult struct {
	SecretKeysLoaded  int
	TemplatesRendered int
}

// RenderTalosTemplates reads the Talos secrets.yaml Secure Note, flattens
// it, substitutes every *.template.yaml file's "{{ dotted.key }}"
// placeholders, and, unless DryRun, writes the rendered files.
//
// The Secure Note's content is real cluster bootstrap material (Talos CA
// keys, tokens, and the like), and the rendered output is that same
// material substituted into otherwise-public template files - so, exactly
// like SyncEnvSecrets, none of it can be allowed to appear as an activity
// result or workflow input: Temporal records both, in plaintext, in its
// durable event history (visible via the Web UI/API/DB in external mode).
// Reading, parsing, rendering, and writing all happen inside this one
// activity call so only a final key/file *count* ever crosses back into
// RenderTalosWorkflow.
func (a *Activities) RenderTalosTemplates(ctx context.Context, in RenderTalosTemplatesInput) (RenderTalosTemplatesResult, error) {
	noteContent, err := a.OnePassword.ReadNote(ctx, in.ItemID)
	if err != nil {
		return RenderTalosTemplatesResult{}, fmt.Errorf("reading 1Password note: %w", err)
	}

	var secretsHash map[string]any
	if err := yaml.Unmarshal([]byte(noteContent), &secretsHash); err != nil {
		return RenderTalosTemplatesResult{}, fmt.Errorf("parsing Secure Note YAML: %w", err)
	}
	flatSecrets := templaterendering.FlattenHash(secretsHash)

	paths, err := a.FS.Glob(filepath.Join(in.TemplateDir, "*.template.yaml"))
	if err != nil {
		return RenderTalosTemplatesResult{}, err
	}

	files := make([]FileWrite, 0, len(paths))
	var allMissing []string

	for _, path := range paths {
		content, err := a.FS.ReadFile(path)
		if err != nil {
			return RenderTalosTemplatesResult{}, fmt.Errorf("reading %s: %w", path, err)
		}

		missing := templaterendering.MissingKeys(content, flatSecrets)
		if len(missing) > 0 {
			allMissing = append(allMissing, missing...)
			continue
		}

		rendered, err := templaterendering.Render(content, flatSecrets)
		if err != nil {
			return RenderTalosTemplatesResult{}, err
		}

		outputName := strings.TrimSuffix(filepath.Base(path), ".template.yaml") + ".yaml"
		files = append(files, FileWrite{
			Path:    filepath.Join(in.TemplateDir, outputName),
			Content: rendered,
		})
	}

	if unresolved := uniqueStrings(allMissing); len(unresolved) > 0 {
		return RenderTalosTemplatesResult{}, fmt.Errorf(
			"cannot hydrate: %d unresolved placeholder(s) - add them to the Secure Note or fix the templates",
			len(unresolved))
	}

	result := RenderTalosTemplatesResult{SecretKeysLoaded: len(flatSecrets), TemplatesRendered: len(files)}
	if in.DryRun {
		return result, nil
	}

	for _, f := range files {
		if err := a.FS.CreateDirectory(filepath.Dir(f.Path)); err != nil {
			return RenderTalosTemplatesResult{}, fmt.Errorf("creating directory for %s: %w", f.Path, err)
		}
		if err := a.FS.WriteFile(f.Path, f.Content); err != nil {
			return RenderTalosTemplatesResult{}, fmt.Errorf("writing %s: %w", f.Path, err)
		}
	}

	return result, nil
}

// uniqueStrings returns items with duplicates removed, preserving first-seen
// order - used to de-duplicate missing placeholder names gathered across
// multiple template files before reporting them.
func uniqueStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, i := range items {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	return out
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
	BaseURL string
}

type RunKeycloakSetupResult struct {
	XML string
	B64 string
}

// RunKeycloakSetup provisions the realm, clients, groups, and seed users,
// and returns the exported SAML descriptor.
//
// The admin credentials are loaded directly from the worker's own
// environment here, rather than received as input. An earlier version had
// a dedicated LoadKeycloakCredentials activity that SetupKeycloakEnvWorkflow
// called first and passed the result into this activity's input - which
// kept the password out of every *other* workflow's history (the original
// problem, since it used to live in the shared LoadConfig result), but
// still let it round-trip through this one workflow's history twice: once
// as LoadKeycloakCredentials's result, once as this activity's own input.
// Loading it directly here means it only ever exists inside this one
// activity call - it never has to cross back into workflow code at all.
func (a *Activities) RunKeycloakSetup(ctx context.Context, in RunKeycloakSetupInput) (RunKeycloakSetupResult, error) {
	cfg, err := config.Load()
	if err != nil {
		return RunKeycloakSetupResult{}, fmt.Errorf("loading keycloak credentials: %w", err)
	}

	client := a.NewKeycloak(in.BaseURL)
	svc := keycloaksetup.New(client, activity.GetLogger(ctx))

	descriptors, err := svc.Setup(ctx, cfg.KeycloakAdmin, cfg.KeycloakAdminPassword)
	if err != nil {
		return RunKeycloakSetupResult{}, err
	}
	return RunKeycloakSetupResult{XML: descriptors.XML, B64: descriptors.B64}, nil
}

// mergeSecrets combines the environment-filtered extract with the
// explicitly-named one, later entries winning by name. Order is preserved so
// the rendered vault item stays stable run to run - a map would reshuffle it
// and produce a meaningless diff every time.
func mergeSecrets(base, override []domain.ExtractedSecret) []domain.ExtractedSecret {
	index := make(map[string]int, len(base))
	merged := make([]domain.ExtractedSecret, 0, len(base)+len(override))

	for _, s := range base {
		index[s.Name] = len(merged)
		merged = append(merged, s)
	}
	for _, s := range override {
		if at, seen := index[s.Name]; seen {
			merged[at] = s
			continue
		}
		index[s.Name] = len(merged)
		merged = append(merged, s)
	}
	return merged
}
