// Package config loads the workflow engine's settings from environment
// variables (optionally via a .env file), mirroring the original tool's
// config/config.rb.
package config

import (
	"fmt"
	"strings"

	"github.com/abradner/workflow/configload"
)

// Config holds every environment-driven setting the workflow engine needs.
type Config struct {
	// SourceDir is the immutable read-only clone.
	SourceDir string `env:"SOURCE_DIR,required"`
	// DestDir is a local clone of the workloads repo (RepoURL) that sync
	// writes app overlays into - a different repo from the one
	// ClusterAppsDir lives in.
	DestDir string `env:"DEST_DIR,required"`
	// ClusterAppsDir is where the ArgoCD App-of-Apps manifests go.
	ClusterAppsDir string `env:"CLUSTER_APPS_DIR,required"`

	// Talos bootstrap configuration.
	TalosItemID      string `env:"OP_TALOS_ITEM_ID"`
	TalosTemplateDir string `env:"TALOS_TEMPLATE_DIR,required"`

	// SourceEnv and Environments drive mapping and extraction.
	SourceEnv    string   `env:"SOURCE_ENV,required"`
	Environments []string `env:"TARGET_ENVS,required" envSeparator:","`

	// Application & environment parameters.
	AppPattern  string `env:"APP_PATTERN,required"`
	ProjectName string `env:"PROJECT_NAME,required"`
	TLD         string `env:"TLD,required"`
	// RepoURL is the git remote for DestDir: what sync commits into, and
	// what the generated ArgoCD ApplicationSet's source.repoURL points
	// ArgoCD at (see argocdApplicationSetManifest) - not the GitOps repo
	// ClusterAppsDir lives in.
	RepoURL string `env:"REPO_URL,required"`

	// AdditionalExactSecrets names AWS secrets that do not follow the
	// environment naming convention and so are invisible to the name filter
	// ExtractSecrets uses. Each entry may contain the placeholder
	// "{{source_env}}", expanded from SourceEnv by Load.
	//
	// The Ruby original used a literal '#{source_env}' inside single quotes -
	// a Ruby interpolation that deliberately does not interpolate, which reads
	// as a bug every time someone new sees it. An explicit placeholder costs
	// nothing and survives being copied into a shell.
	AdditionalExactSecrets []string `env:"ADDITIONAL_EXACT_SECRETS" envSeparator:","`

	// OPVaultName is the 1Password vault sync-1p writes its per-environment
	// Secure Notes into. Deliberately NOT `required`: only sync-1p needs it,
	// and making it mandatory here would fail `sync`, `setup-argo` and
	// `render-talos`, none of which address a vault by name. It is validated
	// at the point of use instead - see activities.SyncEnvSecrets.
	OPVaultName string `env:"OP_VAULT_NAME"`

	// Private container registry.
	RegistryHostname string `env:"REGISTRY_HOSTNAME,required"`
	Registry1PItemID string `env:"REGISTRY_1P_ITEM_ID,required"`

	// Keycloak admin bootstrap credentials.
	KeycloakAdmin         string `env:"KEYCLOAK_ADMIN" envDefault:"admin"`
	KeycloakAdminPassword string `env:"KEYCLOAK_ADMIN_PASSWORD" envDefault:"admin"`

	// ExternalSecretsAPIVersion is parametrized for easy upgrades - not
	// sourced from the environment, set below.
	ExternalSecretsAPIVersion string `env:"-"`
}

// sourceEnvPlaceholder is substituted with Config.SourceEnv in every
// ADDITIONAL_EXACT_SECRETS entry.
const sourceEnvPlaceholder = "{{source_env}}"

// Load reads Config from the environment via the platform's configload
// harness (which loads a .env file first if one exists - missing is fine,
// same as the original tool's dotenv/load), then applies this tool's own
// post-processing.
func Load() (*Config, error) {
	cfg, err := configload.Load[Config]()
	if err != nil {
		return nil, err
	}
	cfg.ExternalSecretsAPIVersion = "external-secrets.io/v1"

	for _, dir := range []*string{&cfg.SourceDir, &cfg.DestDir, &cfg.ClusterAppsDir, &cfg.TalosTemplateDir} {
		expanded, err := configload.ExpandPath(*dir)
		if err != nil {
			return nil, fmt.Errorf("resolving path %q: %w", *dir, err)
		}
		*dir = expanded
	}

	for i, e := range cfg.Environments {
		cfg.Environments[i] = strings.TrimSpace(e)
	}

	// Expanded here rather than at the point of use so every consumer sees a
	// concrete secret name, and a malformed entry surfaces at startup.
	exact := cfg.AdditionalExactSecrets[:0]
	for _, name := range cfg.AdditionalExactSecrets {
		name = strings.TrimSpace(strings.ReplaceAll(name, sourceEnvPlaceholder, cfg.SourceEnv))
		if name != "" {
			exact = append(exact, name)
		}
	}
	cfg.AdditionalExactSecrets = exact

	return cfg, nil
}
