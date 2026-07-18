// Package config loads the workflow engine's settings from environment
// variables (optionally via a .env file), mirroring the original tool's
// config/config.rb.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
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

// Load reads Config from the environment, loading a .env file first if one
// exists (missing is fine - same as the original tool's dotenv/load).
func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	cfg.ExternalSecretsAPIVersion = "external-secrets.io/v1"

	for _, dir := range []*string{&cfg.SourceDir, &cfg.DestDir, &cfg.ClusterAppsDir, &cfg.TalosTemplateDir} {
		expanded, err := expandPath(*dir)
		if err != nil {
			return nil, fmt.Errorf("resolving path %q: %w", *dir, err)
		}
		*dir = expanded
	}

	for i, e := range cfg.Environments {
		cfg.Environments[i] = strings.TrimSpace(e)
	}

	return cfg, nil
}

// expandPath resolves ~ and relative segments to an absolute path, mirroring
// Ruby's File.expand_path.
func expandPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return filepath.Abs(path)
}
