# Antigravity & Gemini Agent Onboarding: Tools Workflow

## Repository Overview
`tools-workflow` is a modular, Ruby-based ETL (Extract, Transform, Load) pipeline designed to bridge legacy deployment manifests with modern ArgoCD & Kustomize GitOps targets. It performs declarative infrastructure refactoring, dynamically migrating manifests into structured namespaces and integrating external Vault/AWS abstractions gracefully.

## Domain Nomenclature
- **Application**: Refers strictly to individual microservices (e.g., `pmn-ext-gw`) and maps identically to the ArgoCD Application schema.
- **Project**: Represents the overall global suite deployment scope.

## File Layout & Separation of Concerns
- **`workflow.rb`**: Pipeline CLI entry point.
- **`config/config.rb`**: Parses `.env` settings bounding the extraction (source) and deployment (target) directories (`athena-gitops`, `pmn-workloads`).
- **`app/workflow/hydrate/`**: The **Extract** phase. Responsible for deep-recursing source overlays (e.g., `WorkspaceExtractor`) and mapping physical file assets into memory safely.
- **`app/workflow/transformers/`**: The **Transform** phase. Highly decoupled mutations operating entirely in memory on `AppManifestWorkspace` payloads:
  - `EnvironmentGenerator`: Extrapolates targeted overlay configurations from a base physical source context.
  - `LegacyModernizer`: Rewrites outdated Kustomize structures, upgrades API versions, and natively strips conflicting fields (like `topologySpreadConstraints`).
  - `ServiceAbstractionLinker`: Intercepts `ConfigMap` literals to decouple DNS resolutions, producing cluster-local `.svc.cluster.local` targets while securely bridging ExternalName Service deployments downstream.
  - `PullSecretInjector`: Synthesizes dynamic 1Password Vault targets mapping towards unique application `registry-pull-secret` keys.
- **`app/services/filesystem_service.rb`**: The **Load** phase engine. The singular truth for robust filesystem rendering. Deeply intelligent serialization dynamically generating cleanly delimited Kubernetes `---` Multi-Document Streams vs exact JSON Patch Arrays.
- **`spec/`**: Robust RSpec unit verification suite.

## Agent Guidelines & Operation
- **ETL Contract**: The pipeline enforces a strict Extract-Transform-Load decoupling. 
  - **State Coordination**: Orchestrators coordinate the sequence of E/T/L steps and hold the overarching execution state. Business logic MUST be segregated into workflow-agnostic classes.
  - **Extraction (`Hydrate::`)**: `WorkspaceExtractor` is one implementation within the `Hydrate` module, not a god-class. It performs extraction and maps YAML functionally.
  - **Mutation Integration**: The `Transformers` execute pure mutations sequentially returning state.
  - **Side-effects**: `commit_phase` executes IO across planned workspaces. Never introduce filesystem reads/writes inside `Transformers`.
- **Testing Requirements**: The test suite must focus on **quality over quantity**. While having baseline coverage on everything is essential, the suite should intentionally grow only to capture novel scenarios or edge cases, rather than artificially inflating test volume. Add, rewrite, split, or delete tests to ensure the suite accurately verifies behaviors and catches regressions. Workflows get integration tested, services get behaviourally tested, and utils get unit tested. Anything that 'leaks' from the test environment gets mocked at the IO boundary. Stop `ENV` leaks natively in RSpec by explicitly mounting properties via Config mock injections (e.g., `allow(cfg).to receive(:project_name)`).
- **YAML Mutation**: Use the `mutate_yaml(&block)` wrapper in `Base` transformer explicitly when augmenting documents sequentially to prevent string-iteration crashes (`NoMethodError: map!`) upon raw text files.

## Recent Snapshot Learnings & Insights
1. **Namespace Collisions (ArgoCD)**: Global resources (like Kafka, Postgres bindings, or generic pull secrets) deployed across intersecting environments throw `"Shared Service Warnings"` in ArgoCD. Always enforce explicit app-prefixing onto these resources metadata logic (e.g.: `#{app_name}-registry`, `#{app_name}-pg`) directly to segregate ownership across distinct Argo Applications gracefully.
2. **Vault API Rate Limiting**: Repeated concurrent syncing over identical ExternalSecret targets triggers `rate limit exceeded`. Force `refreshInterval: '24h'` within `LegacyModernizer` and pipeline injectors cleanly.
3. **YAML Document Streams**: Kustomize base resources natively reject sequences `[{kind: Resource}]` complaining of `missing Resource metadata`. `FilesystemService` intelligently traps Array arrays with K8s hashes and serializes them natively joined over `.sub(/\A---\n/, '')` standard streams.
4. **Filesystem Abstraction**: Emphasize that all file I/O must route through `FilesystemService` to maintain completely mockable unit test boundaries natively. No real disk I/O should ever occur during RSpec testing.
5. **Service Layering**: Detail that low-level bash/CLI wrappers (`ServiceClients`) strictly decouple from high-level mapping/business logic (`Services`) to enable native integration testing without brittle string manipulation in `Open3`.
6. **1Password Secret Layout**: Document the "One Item per Environment" strategy (`k8s-<project_name>-<env>`) where AWS contexts map onto 1Password "Sections" and keys map onto "Fields".
