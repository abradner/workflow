# Workflow

Ruby ETL pipeline for managing Kustomize workloads, ArgoCD manifests, 1Password secrets, Keycloak realm setup, and Talos cluster bootstrapping.

## Requirements

- Ruby 4.0+ (managed via [mise](https://mise.jdx.dev/))
- 1Password CLI (`op`) for secret operations
- AWS CLI for Secrets Manager extraction
- Bundler for dependencies

```bash
bundle install
```

## Usage

```bash
./workflow.rb <command> [--dry-run] [--verbose]
```

### Commands

| Command | Orchestrator | Description |
|---------|-------------|-------------|
| `sync` | `SyncWorkloads` | Discovers Kustomize apps, clones overlays per target environment, modernises manifests (Ingress→HTTPRoute, ExternalSecret upgrades), injects registry pull-secrets, writes to destination. |
| `setup-argo` | `GenerateArgocd` | Generates ArgoCD `Application` manifests for each app×environment. Writes to the cluster apps directory. |
| `sync-1p` | `Sync1Password` | Extracts secrets from AWS Secrets Manager, maps them onto `Domain::OnePassword::Item` objects, and upserts structured Secure Notes in 1Password (one item per environment: `k8s-<project>-<env>`). |
| `setup-keycloak` | `SetupKeycloak` | Initialises Keycloak realms and clients per environment, exports SAML descriptors to the workloads repo. |
| `render-talos` | `RenderTalos` | Reads a 1Password Secure Note, flattens its YAML to dotted keys, and substitutes `{{ key }}` placeholders in `.template.yaml` files. |

## Architecture

Each orchestrator follows an ETL pattern with three phases:

1. **Hydrate** — Prerequisite data loading. Orchestrators declare dependencies via `needs :predicate_name` and the Runner satisfies them before execution.
2. **Act** — Side-effect-free transformation and planning. All logic runs in memory.
3. **Commit** — Filesystem writes, API calls, vault mutations. Skipped with `--dry-run`.

### Project Structure

```
workflow.rb                          # CLI entry point
config/config.rb                     # Environment-driven configuration

app/
  domain/
    one_password/item.rb             # 1Password item with field-level upsert tracking
    kubernetes/external_secret.rb    # ExternalSecret manifest builder
    saml_credentials.rb              # SAML key/descriptor value object

  workflow/
    runner.rb                        # Phase executor and hydration coordinator
    execution_context.rb             # Shared state container
    orchestrator.rb                  # Base class with `needs` declarations
    orchestrators/                   # One per command
    hydrate/                         # Discovery, SamlCredentials, OnePasswordItems, WorkspaceExtractor
    transformers/                    # EnvironmentGenerator, LegacyModernizer, ServiceAbstractionLinker,
                                     # PullSecretInjector, OnePasswordItemMapper, OnePasswordSamlKeyInjector

  services/                         # Business logic
    filesystem_service.rb            # All file I/O
    one_password_commit_service.rb   # Vault write orchestration (edit vs create)
    aws_secrets_service.rb           # AWS Secrets Manager extraction
    keycloak_setup_service.rb        # Realm/client provisioning
    template_rendering_service.rb    # Dotted-key flattening and placeholder substitution
    discover_saml_creds_service.rb   # Keycloak SAML descriptor fetcher
    endpoint_mapper.rb               # Hostname suffix matching

  service_clients/                   # Low-level CLI/HTTP wrappers
    op.rb                            # 1Password CLI (create_item, edit_item, get_item, read_note)
    aws.rb                           # AWS CLI
    keycloak.rb                      # Keycloak REST API

spec/                                # RSpec suite
```

## Configuration

Copy `.env.example` to `.env` and fill in the values. All config is loaded via `dotenv` gem.

```dotenv
# Directories
SOURCE_DIR='/path/to/source/workloads'       # Read-only source repo
DEST_DIR='/path/to/dest/workloads'           # GitOps destination for rendered manifests
CLUSTER_APPS_DIR='/path/to/gitops/cluster/apps'  # ArgoCD Application manifests
TALOS_TEMPLATE_DIR='/path/to/talos/templates'    # Talos .template.yaml files

# Environment mapping
SOURCE_ENV='dev3'                    # Environment to extract from
TARGET_ENVS='dev4,dev5'              # Comma-separated target environments
APP_PATTERN='pmn-*'                  # Glob for discovering Kustomize apps
PROJECT_NAME='pmn'                   # Namespace prefix: <project>-<env>
TLD='example.tld'                    # Top-level domain for generated routes

# Registry
REGISTRY_HOSTNAME='cr.example.com'
REGISTRY_1P_ITEM_ID='<1p-item-id>'

# 1Password
OP_VAULT_NAME='Tooling'              # Target vault for sync-1p
OP_TALOS_ITEM_ID='<1p-item-id>'      # Secure Note ID for render-talos

# Git
REPO_URL='https://github.com/org/gitops.git'

# Keycloak (optional, defaults to 'admin')
KEYCLOAK_ADMIN='admin'
KEYCLOAK_ADMIN_PASSWORD='admin'
```

## Testing

```bash
bundle exec rspec
```

All I/O routes through `FilesystemService`, which is mocked at test boundaries. No real disk, network, or vault calls during test runs.
