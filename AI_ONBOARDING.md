# Antigravity & Gemini Agent Onboarding: Tools Workflow

## Repository Overview
`tools-workflow` is a modular, Ruby-based automation pipeline. It operates on a loosely-applied Extract-Transform-Load (ETL) pattern, orchestrating declarative infrastructure refactoring, manifest migration, secret management, and Talos cluster bootstrapping across related GitOps repositories.

## Domain Nomenclature
- **Application**: An individual microservice (e.g., `pmn-ext-gw`), mapping directly to the ArgoCD Application schema.
- **Project**: The overall deployment suite scope. `Config#project_name` (from the `PROJECT_NAME` env var) is the canonical identifier used for namespace interpolation: `#{project_name}-#{env}`.

## File Layout & Separation of Concerns

### Pipeline Entry Point
- **`workflow.rb`**: CLI entry point. Routes commands to orchestrators.
- **`config/config.rb`**: Parses `.env` settings bounding extraction (source) and deployment (target) directories, 1Password item IDs, registry config, and cross-repo paths. All inter-repository relationships are configured here via environment variables — no hardcoded paths.

### Extract Phase
- **`app/workflow/hydrate/`**: State hydration for orchestrators that need discovered context before acting. `Discovery` scans the source directory for applications matching `APP_PATTERN`. `WorkspaceExtractor` recurses base and source overlay directories, loading all files into an in-memory `AppManifestWorkspace`.

### Transform Phase
- **`app/workflow/transformers/`**: Pure, sequential, side-effect-free mutations operating on `AppManifestWorkspace` payloads:
  - `EnvironmentGenerator`: Deep-clones source configurations to produce per-target-environment overlays. **Must run first**.
  - `LegacyModernizer`: Rewrites outdated Kustomize structures, upgrades API versions, converts `Ingress` to `HTTPRoute`, and strips conflicting fields.
  - `ServiceAbstractionLinker`: Intercepts `ConfigMap` literals to decouple DNS resolutions, producing cluster-local `.svc.cluster.local` targets while generating `ExternalName` Service overlays downstream.
  - `PullSecretInjector`: Synthesises per-app `ExternalSecret` resources for registry pull-secret credentials backed by 1Password.

### Domain Models
- **`app/domain/kubernetes/`**: Typed domain models using Ruby's `Data.define` for structurally correct K8s resource generation:
  - `ExternalSecret`: Generates well-formed ExternalSecret manifests targeting the `ClusterSecretStore`.
  - `HTTPRoute`: Generates Gateway API HTTPRoute resources, including conversion from legacy Ingress specs.
  
  Use domain models when a K8s resource has structural complexity worth encapsulating (nested specs, computed fields). Add new models when transformers start building deeply nested hashes inline — that's the signal to extract.

### Load Phase
- **`app/services/filesystem_service.rb`**: The singular truth for filesystem I/O. Handles YAML serialisation (multi-document streams vs JSON Patch arrays), directory creation, and file reading.

### Services & Clients
- **`app/services/`**: High-level business logic services:
  - `TemplateRenderingService`: Flattens nested YAML hashes to dot-notation keys and renders `{{ dotted.key }}` template placeholders. Used by `RenderTalos`.
  - `OnePasswordService`: Builds structured 1Password item payloads for vault provisioning.
  - `AwsSecretsService`: Extracts secrets from AWS Secrets Manager. Actively used — the upstream AWS clusters remain live and any changes must be resynced via `sync-1p`.
  - `EndpointMapper`: Suffix-based hostname matching for known external service types (pg, kafka, redis).

- **`app/service_clients/`**: Low-level CLI wrappers strictly decoupled from business logic:
  - `Op`: 1Password CLI wrapper — `create_item` for item provisioning, `read_note` for Secure Note retrieval.
  - `Aws`: AWS CLI wrapper for Secrets Manager operations.

### Testing
- **`spec/`**: RSpec test suite. Workflows get integration tested, services get behaviourally tested, and utils get unit tested. All file I/O routes through `FilesystemService`, which is mocked at test boundaries — no real disk I/O during RSpec runs.

## Orchestrators

Four commands are available via `workflow.rb [command]`:

### `sync` → `SyncWorkloads`
Full ETL pipeline: discovers apps via `Hydrate::Discovery`, extracts workspaces, runs the transformer chain sequentially, and commits rendered manifests to the destination directory. This is the most complex orchestrator and the canonical example of the ETL pattern.

### `setup-argo` → `GenerateArgocd`
Generates ArgoCD `Application` manifests for each app×environment combination. Requires `Hydrate::Discovery` to know which apps exist. Writes to `cluster/apps/` in the `athena-gitops` repo.

### `sync-1p` → `Sync1Password`
Extracts secrets from AWS Secrets Manager and provisions structured Secure Notes in 1Password ("One Item per Environment": `k8s-<project_name>-<env>`). Does **not** require hydration — operates independently of app discovery. Actively used for ongoing AWS→1P synchronisation. **Note:** this orchestrator would benefit from a minor refactor to align more closely with the ETL paradigm used by the other orchestrators.

### `render-talos` → `RenderTalos`
Reads a 1Password Secure Note containing full `secrets.yaml` content, flattens it to dot-notation keys, and substitutes `{{ dotted.key }}` placeholders in `.template.yaml` files to produce hydrated Talos cluster configs. Does **not** use the `Hydrate::` phase or `Transformers` — it follows its own extract/render/write flow. **Note:** this orchestrator would benefit from a minor refactor to align more closely with the ETL paradigm used by the other orchestrators.

## Architecture: Orchestrator Contract

Orchestrators inherit from `Workflow::Orchestrator` and implement two phase methods:

```ruby
class MyOrchestrator < Workflow::Orchestrator
  needs :discovery_completed  # Declare predicates the Runner must satisfy

  def act_phase(context)      # Side-effect-free planning and validation
  end

  def commit_phase(context)   # Filesystem writes, API calls, side effects
  end
end
```

**Predicate hydration**: Orchestrators declare dependencies via `needs :predicate_name`. The `Runner` satisfies predicates in order using `HYDRATION_ACTIONS` (defined in `runner.rb`) before executing phases. Orchestrators without `needs` declarations (e.g., `Sync1Password`, `RenderTalos`) skip hydration entirely.

**Constructor contract**: All orchestrators receive `config:` as a required keyword argument, validated as an instance of `Config`.

## Agent Guidelines & Operation

- **ETL Discipline**: Extraction and transformation must remain side-effect-free in `act_phase`. All read I/O belongs in the hydrate (discovery/extraction) phase. All write I/O belongs in `commit_phase`. Never introduce filesystem reads/writes inside `Transformers`.
- **Transformer Ordering**: `EnvironmentGenerator` must sequence first (in workflows that use it) — it deep-clones source configurations. Subsequent transformers operate on the fully expanded workspace independently.
- **YAML Mutation**: Use the `mutate_yaml(&block)` wrapper in `Base` transformer when augmenting documents sequentially. This prevents `NoMethodError: map!` crashes on raw text files by normalising single Hash vs Array document streams.
- **Testing Requirements**: Focus on quality over quantity. Add, rewrite, split, or delete tests to accurately verify behaviours and catch regressions — don't inflate test volume artificially. Stop `ENV` leaks by injecting properties via Config mock injections (e.g., `allow(cfg).to receive(:project_name)`).
- **Filesystem Abstraction**: All file I/O must route through `FilesystemService` to maintain completely mockable test boundaries.
- **Service Layering**: `ServiceClients` (bash/CLI wrappers, http clients etc) are strictly decoupled from `Services` (business logic). This enables integration testing without brittle `Open3` string manipulation.

## Secret Strategies

Two distinct patterns are in use:

1. **Workload Secrets** (read by the cluster, example in `sync-1p`): "One Item per Environment" in 1Password (`k8s-<project_name>-<env>`). AWS Secrets Manager contexts map onto 1Password Sections; keys map onto Fields. ExternalSecrets in the cluster reference these items via the `onepassword-backend` `ClusterSecretStore`.

2. **Talos Bootstrap Secrets** (read by the workflow, example in `render-talos`): A single 1Password Secure Note containing full `secrets.yaml` YAML content. The `TemplateRenderingService` flattens this to dotted keys for template substitution. The item ID is configured via `OP_TALOS_ITEM_ID` in `.env`.

## Recent Snapshot Learnings & Insights
1. **Namespace Collisions (ArgoCD)**: Global resources deployed across intersecting environments trigger ArgoCD shared resource warnings. Enforce explicit app-prefixing on all resource metadata (e.g., `#{app_name}-registry`, `#{app_name}-pg`) to segregate ownership across distinct Argo Applications.
2. **1Password API Rate Limiting**: Concurrent syncing over identical ExternalSecret targets triggers rate limit errors. Force `refreshInterval: '24h'` within `LegacyModernizer` and pipeline injectors.
3. **YAML Document Streams**: Kustomize rejects sequences `[{kind: Resource}]` with `missing Resource metadata`. `FilesystemService` serialises Array payloads as standard multi-document YAML streams (`.sub(/\A---\n/, '')`).
4. **Filesystem Abstraction**: All file I/O must route through `FilesystemService` to maintain completely mockable unit test boundaries. No real disk I/O during RSpec testing.
