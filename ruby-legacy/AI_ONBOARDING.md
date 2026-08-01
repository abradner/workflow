# Agent Onboarding: Tools Workflow

## Overview
Ruby ETL pipeline. Orchestrates Kustomize manifest migration, ArgoCD app generation, 1Password secret sync, Keycloak realm setup, and Talos cluster bootstrapping across related GitOps repos.

## Nomenclature
- **Application**: Individual microservice (e.g. `pmn-ext-gw`), maps 1:1 to an ArgoCD Application.
- **Project**: Deployment suite scope. `Config#project_name` → namespace interpolation: `#{project_name}-#{env}`.

## File Layout

### Entry
- `workflow.rb` — CLI routing.
- `config/config.rb` — All config from `.env`. No hardcoded paths.

### Hydrate (`app/workflow/hydrate/`)
State loading before orchestrators run. Orchestrators declare what they need; the Runner satisfies it.
- `Discovery` — scans `SOURCE_DIR` for apps matching `APP_PATTERN`.
- `WorkspaceExtractor` — recurses base + overlay dirs into in-memory `AppManifestWorkspace`.
- `SamlCredentials` — fetches Keycloak SAML descriptors and public keys per environment.
- `OnePasswordItems` — queries `op item get` to build `Domain::OnePassword::Item` objects per environment.

### Transform (`app/workflow/transformers/`)
Pure, side-effect-free mutations. Operate on in-memory state only.
- `EnvironmentGenerator` — deep-clones source to per-target overlays. **Must run first** when used.
- `LegacyModernizer` — API version upgrades, Ingress→HTTPRoute, strips conflicting fields.
- `ServiceAbstractionLinker` — rewrites ConfigMap DNS literals to `.svc.cluster.local`, generates ExternalName Services.
- `PullSecretInjector` — synthesises per-app ExternalSecret for registry pull-secrets.
- `OnePasswordItemMapper` — maps AWS-extracted secrets onto `Domain::OnePassword::Item` via `#upsert_field`.
- `OnePasswordSamlKeyInjector` — injects Keycloak public keys into JSON secret payloads.

### Domain (`app/domain/`)
Typed models for structurally complex resources. Extract a new one when transformers start building deeply nested hashes inline.
- `kubernetes/ExternalSecret` — `Data.define`, generates well-formed ExternalSecret manifests.
- `kubernetes/HTTPRoute` — Gateway API HTTPRoute, including Ingress conversion.
- `one_password/Item` — mutable item model with field-level upsert, section tracking, and stale-field detection.
- `SamlCredentials` — SAML key/descriptor value object.

### Services (`app/services/`)
Business logic, decoupled from I/O mechanics.
- `FilesystemService` — **all** file I/O goes through here. YAML serialisation, directory ops, file reads.
- `OnePasswordCommitService` — dispatches `edit_item` (existing) or `create_item` (new) based on domain object state.
- `AwsSecretsService` — extracts from AWS Secrets Manager.
- `KeycloakSetupService` — realm initialisation, client/user mapping, SAML descriptor export.
- `TemplateRenderingService` — dot-notation flattening and `{{ key }}` placeholder substitution.
- `DiscoverSamlCredsService` — Keycloak SAML descriptor fetcher.
- `EndpointMapper` — suffix-based hostname matching (pg, kafka, redis).

### Service Clients (`app/service_clients/`)
Low-level CLI/HTTP wrappers. No business logic.
- `Op` — 1Password CLI: `create_item`, `edit_item`, `get_item`, `read_note`.
- `Aws` — AWS CLI for Secrets Manager.
- `Keycloak` — Keycloak REST API (readiness, realm descriptor, public key).

## Orchestrators

Five commands via `workflow.rb <command>`:

| Command | Orchestrator | Needs | What it does |
|---------|-------------|-------|-------------|
| `sync` | `SyncWorkloads` | `discovery_completed` | Full ETL: discover apps, extract workspaces, run transformer chain, write manifests. |
| `setup-argo` | `GenerateArgocd` | `discovery_completed` | Generate ArgoCD `Application` manifests per app×env. |
| `sync-1p` | `Sync1Password` | `saml_credentials_extracted`, `one_password_items_hydrated` | AWS→1Password secret sync. One Secure Note per env (`k8s-<project>-<env>`). Upserts via `op item edit`. |
| `setup-keycloak` | `SetupKeycloak` | — | Initialise Keycloak realms/clients, export SAML descriptors to workloads repo. |
| `render-talos` | `RenderTalos` | — | Read 1P Secure Note, flatten YAML, substitute `{{ key }}` placeholders in `.template.yaml` files. |

## Orchestrator Contract

```ruby
class MyOrchestrator < Workflow::Orchestrator
  needs :discovery_completed      # Runner satisfies before execution

  def act_phase(context)          # Side-effect-free planning
  end

  def commit_phase(context)       # I/O, API calls, mutations
  end
end
```

Constructor receives `config:` (validated `Config` instance). The Runner resolves `needs` predicates via `HYDRATION_ACTIONS` in `runner.rb`.

## Rules

- **ETL boundaries**: Read I/O in hydrate. Transforms in `act_phase` (no side effects). Write I/O in `commit_phase` only.
- **Transformer ordering**: `EnvironmentGenerator` first (when used) — it deep-clones the workspace. Others run independently after.
- **YAML safety**: Use `mutate_yaml(&block)` in transformers to handle Hash vs Array document streams without crashing on non-YAML files.
- **Filesystem abstraction**: All file I/O through `FilesystemService`. No exceptions.
- **Service layering**: `ServiceClients` (CLI/HTTP) are strictly decoupled from `Services` (logic).
- **Domain objects are not mocked in tests.** Use real instances. Their internal state tracking (field IDs, touched tracking) is the point.
- **Testing**: Quality over quantity. Mock at I/O boundaries (Config, ServiceClients, FilesystemService). Inject config via `allow(cfg).to receive(...)` — never leak `ENV`.
- **RDoc expectations (What/Why/How)**: Wherever there is a subtle, non-obvious, or complex method/behaviour, explicitly utilize standard RDoc blocks featuring:
  - `What`: Simple objective/context of what it does
  - `Why`: What would break or not happen without this code? Focus on functional consequences, not refactoring history
  - `How`: An explanation of the behaviour in simple, quickly-grokable terms so fellow agents/engineers understand the pipeline magic.

## Secret Strategies

1. **Workload secrets** (`sync-1p`): One 1Password item per environment (`k8s-<project>-<env>`). AWS secret names → Sections, keys → Fields. Cluster reads via `onepassword-backend` ClusterSecretStore + ExternalSecrets.
2. **Talos secrets** (`render-talos`): Single 1P Secure Note with full `secrets.yaml`. Flattened to dotted keys for template substitution. Item ID from `OP_TALOS_ITEM_ID`.

## Known Gotchas

1. **ArgoCD namespace collisions**: Prefix all resource metadata with app name (e.g. `#{app_name}-registry`) to avoid shared-resource warnings across environments.
2. **1Password rate limiting**: ExternalSecret `refreshInterval` forced to `24h` to avoid hammering the vault.
3. **YAML document streams**: Kustomize rejects `[{kind: ...}]` arrays. `FilesystemService` serialises as multi-document streams (strips leading `---`).
