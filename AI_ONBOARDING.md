# Agent Onboarding: Workflow

## Repository Overview

`workflow` is a Go + Temporal ETL pipeline: five workflows that discover, extract, transform, and
commit Kubernetes/GitOps manifests, AWS→1Password secret migrations, Talos template hydration, and
Keycloak provisioning. It's a from-scratch Go rebuild of an earlier Ruby version (see git history
before this rebuild) - same jobs, same `.env` configuration surface, same output; Temporal replaces
the Ruby version's hand-rolled `Runner`/`Orchestrator`/`needs`-predicate framework. See
`docs/MIGRATION_PLAYBOOK.md` for the reusable, repo-agnostic version of this migration's concept map
and lessons learned - written for reuse on a future migration of a different Ruby tool.

## Domain Nomenclature

- **Application**: An individual microservice (e.g. `pmn-ext-gw`), mapping to the ArgoCD
  Application schema.
- **Project**: The overall deployment suite scope. `Config.ProjectName` (`PROJECT_NAME`) is the
  canonical identifier used for namespace interpolation: `<project>-<env>`.

## File Layout & Separation of Concerns

- **`cmd/workflow/`**: CLI entry point (cobra). Each subcommand loads `Config`, builds that
  workflow's input, and runs it via `internal/temporalutil` - embedded (in-process dev server) or
  external (dial an existing server; pairs with the `worker` subcommand).
- **`internal/config/`**: Env-driven `Config` struct (`.env` via godotenv + struct tags via
  caarlos0/env). One flat struct threaded through workflow → activity inputs as needed.
- **`internal/domain/`**: Small framework-free value types shared across packages -
  `SamlCredentials`, `ExtractedSecret`, and `internal/domain/kubernetes` (`ExternalSecret`,
  `HTTPRoute` manifest builders).
- **`internal/manifest/`**: The `Workspace` model (path → parsed-YAML-or-string, mirroring Ruby's
  `AppManifestWorkspace`) plus pure helpers: `Dig`/`DigMap`/`DigString`/`DigSlice` (Ruby
  `Hash#dig`-style nested access), `MutateYAML` (normalizes single-doc vs. document-stream), and
  `RenderYAML`/`IsYAMLPath` (pure YAML text rendering - no I/O).
- **`internal/transformers/`**: The pure, sequential manifest-mutation pipeline - direct
  equivalents of Ruby's `Workflow::Transformers::*`. Every function/type here takes a
  `*manifest.Workspace` and returns it mutated, with **no I/O and no Temporal dependency** - fully
  unit-testable standalone.
- **`internal/serviceclients/`**: Low-level wrappers strictly decoupled from business logic - `op`
  (1Password CLI via `os/exec`), `keycloak` (plain `net/http`).
- **`internal/services/`**: Business logic built on the service clients - `filesystem` (the only
  place raw disk I/O happens outside activities), `awssecrets` (AWS SDK v2, not a CLI wrapper),
  `onepassword`, `templaterendering`, `keycloaksetup`, `discoversamlcreds`, `endpointmapper`,
  `workspaceextractor`.
- **`internal/activities/`**: Every Temporal activity - the only place non-determinism/I-O is
  allowed to happen from a workflow's perspective. Methods on `*Activities` so real dependencies
  swap for fakes in tests.
- **`internal/workflows/`**: The five Temporal workflows, one per CLI command. See the package doc
  comment in `internal/workflows/support.go` and each file's doc comment for what it does and why
  it's structured the way it is.
- **`internal/temporalutil/`**: Wires up the two run modes (embedded via
  `go.temporal.io/server/temporaltest`, external via `client.Dial`) and the shared
  workflow/activity registration list.

## Ruby → Go File Map

The full pre-rebuild Ruby implementation is kept at [`ruby-legacy/`](ruby-legacy/) for side-by-side
reference (see the README's note on it). This is the file-by-file correspondence.

**CLI entry point**

| Ruby (`ruby-legacy/`) | Go |
| --- | --- |
| `workflow.rb` | `cmd/workflow/main.go`, `root.go`, `commands.go`, `run.go`, `worker.go` |
| `config/config.rb` | `internal/config/config.go` |

**Framework - superseded, not ported 1:1** (Temporal's workflow/activity model replaces the concept
entirely; see "Architecture: Workflow Contract" below)

| Ruby | Go |
| --- | --- |
| `app/workflow/orchestrator.rb` | *(none - `internal/workflows/*.go` are plain functions, no base class)* |
| `app/workflow/runner.rb` | *(none - `internal/temporalutil/` + Temporal itself)* |
| `app/workflow/execution_context.rb` | *(none - `workflow.Context` plus each workflow's own `Input`/`Result` structs)* |

**Hydration → Activities** (no longer a separate predicate-driven phase - each workflow just calls
the activity it needs)

| Ruby | Go |
| --- | --- |
| `app/workflow/hydrate/discovery.rb` | `internal/activities/activities.go` (`DiscoverApps`) |
| `app/workflow/hydrate/saml_credentials.rb` | `internal/activities/activities.go` (`FetchSamlCredentials`) + `internal/services/discoversamlcreds/service.go` |
| `app/workflow/hydrate/workspace_extractor.rb` | `internal/services/workspaceextractor/extractor.go` |
| `app/workflow/models/app_manifest_workspace.rb` | `internal/manifest/workspace.go` |

**Orchestrators → Workflows** (one per CLI command)

| Ruby | Go |
| --- | --- |
| `app/workflow/orchestrators/sync_workloads.rb` | `internal/workflows/syncworkloads.go` |
| `app/workflow/orchestrators/generate_argocd.rb` | `internal/workflows/generateargocd.go` |
| `app/workflow/orchestrators/sync_1password.rb` | `internal/workflows/sync1password.go` |
| `app/workflow/orchestrators/render_talos.rb` | `internal/workflows/rendertalos.go` |
| `app/workflow/orchestrators/setup_keycloak.rb` | `internal/workflows/setupkeycloak.go` |

**Transformers** (pure manifest-mutation pipeline, run inline from `BuildAppFiles` - see below)

| Ruby | Go |
| --- | --- |
| `app/workflow/transformers/base.rb` | `internal/manifest/dig.go` (`MutateYAML`) |
| `app/workflow/transformers/environment_generator.rb` | `internal/transformers/environment_generator.go` |
| `app/workflow/transformers/legacy_modernizer.rb` | `internal/transformers/legacy_modernizer.go` |
| `app/workflow/transformers/pull_secret_injector.rb` | `internal/transformers/pull_secret_injector.go` |
| `app/workflow/transformers/service_abstraction_linker.rb` | `internal/transformers/service_abstraction_linker.go` |
| `app/workflow/transformers/one_password_saml_key_injector.rb` | `internal/transformers/onepassword_saml_key_injector.go` |

**Domain**

| Ruby | Go |
| --- | --- |
| `app/domain/saml_credentials.rb` | `internal/domain/saml_credentials.go` |
| `app/domain/kubernetes/external_secret.rb` | `internal/domain/kubernetes/external_secret.go` |
| `app/domain/kubernetes/http_route.rb` | `internal/domain/kubernetes/http_route.go` |
| *(inline `{name:, string:, binary:}` hashes)* | `internal/domain/extracted_secret.go` *(new: given an explicit type)* |

**Services**

| Ruby | Go |
| --- | --- |
| `app/services/filesystem_service.rb` | `internal/services/filesystem/service.go` |
| `app/services/aws_secrets_service.rb` | `internal/services/awssecrets/service.go` *(now calls the AWS SDK v2 directly)* |
| `app/services/one_password_service.rb` | `internal/services/onepassword/service.go` |
| `app/services/template_rendering_service.rb` | `internal/services/templaterendering/service.go` |
| `app/services/keycloak_setup_service.rb` | `internal/services/keycloaksetup/service.go` |
| `app/services/discover_saml_creds_service.rb` | `internal/services/discoversamlcreds/service.go` |
| `app/services/endpoint_mapper.rb` | `internal/services/endpointmapper/endpoint_mapper.go` |

**Service clients**

| Ruby | Go |
| --- | --- |
| `app/service_clients/aws.rb` | *(none - folded into `internal/services/awssecrets`; no separate CLI-wrapper client since it's SDK-based now)* |
| `app/service_clients/op.rb` | `internal/serviceclients/op/client.go` |
| `app/service_clients/keycloak.rb` | `internal/serviceclients/keycloak/client.go` |

**Utils**

| Ruby | Go |
| --- | --- |
| `app/utils/colorized_logger.rb` | `internal/logging/logger.go` |

**Tests**: each `spec/**/*_spec.rb` has a same-purpose `*_test.go` next to the Go file it covers
(e.g. `spec/services/one_password_service_spec.rb` ↔ `internal/services/onepassword/service_test.go`);
`spec/orchestrators/*_spec.rb` and `spec/workflow_e2e_spec.rb` are covered by
`internal/workflows/*_test.go` using Temporal's `testsuite` package instead of RSpec doubles.

**New in Go, no Ruby counterpart**: `internal/activities/` (the Temporal I/O boundary itself -
Ruby had no equivalent framework concept) and `internal/temporalutil/` (embedded/external Temporal
wiring).

## Architecture: Workflow Contract

There's no orchestrator base class or `needs` predicate list to satisfy - each workflow is an
ordinary Go function:

```go
func SyncWorkloadsWorkflow(ctx workflow.Context, in SyncWorkloadsInput) (SyncWorkloadsResult, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	// ... call activities, run pure logic, respect in.DryRun ...
}
```

**Dry-run semantics are per-workflow, not uniform** - they mirror exactly where the original Ruby
code put real work (`act_phase` vs `commit_phase`):

- `SyncWorkloads`, `GenerateArgocd`, `Sync1Password`, `RenderTalos`: discovery/extraction/validation
  always runs; only the final write/ingest activity is skipped under `DryRun`.
- `SetupKeycloak`: essentially **all** of its work was in Ruby's `commit_phase`, so `DryRun` skips
  the entire environment loop, not just a write step.

**Config is loaded by an activity (`LoadConfig`), not passed in as workflow input**: every
workflow's first call is `runActivity[activities.LoadConfigResult](ctx, a.LoadConfig)`. This isn't
optional plumbing - a Temporal client (the CLI command that starts a workflow) and the worker that
executes it can be different machines with different filesystems entirely (that's the whole point
of `--temporal=<host:port>` mode: docker-compose runs the worker in a container with its own
mounted paths). If the CLI loaded `Config` itself and passed it in as workflow input, every
filesystem path in it would reflect the *client's* machine, not the worker's - activities would
then look for source manifests or write output using the wrong process's paths. `LoadConfig` runs
wherever the activity actually executes (the worker), so `Config` always matches the filesystem
doing the real work, regardless of where the workflow was started from.

**Determinism discipline**: workflow code must never range over a Go map to decide the
order/arguments of activity calls - Go's map iteration is deliberately randomized, and Temporal
replay requires a workflow to reissue the same command sequence on every replay. Either keep the
map consumption inside an activity (it's never replayed), or convert to a sorted slice first (see
`manifest.Workspace.SortedPaths`).

**Why `BuildAppFiles` bundles extract+transform+render into one activity**: the transformer
pipeline is pure and could run directly in workflow code, but the manifests it operates on are
parsed YAML that may contain integers (e.g. HTTPRoute port numbers). Once that data crosses the
workflow/activity boundary, Temporal's default JSON data converter loses the int/float distinction
(decoding a JSON number into `any` always yields `float64`), which would silently corrupt e.g.
`port: 80` into `port: 80.0` in the rendered YAML. Keeping extract+transform+render together means
only final, already-rendered strings ever cross the boundary. `GenerateArgocd`'s manifest, by
contrast, is built fresh with no numeric fields at all, so it's rendered directly in workflow code
with no such risk - see the comments in `internal/activities/activities.go` and
`internal/workflows/generateargocd.go`.

**Three of the five workflows fan out to per-unit child workflows** (`SyncWorkloads` → per-app
`SyncAppWorkflow`, `SetupKeycloak` → per-env `SetupKeycloakEnvWorkflow`, `Sync1Password` → per-env
`Sync1PasswordEnvWorkflow`), started concurrently via `workflow.ExecuteChildWorkflow` and awaited
afterward. `GenerateArgocd` and `RenderTalos` deliberately stay as single linear workflows - they
have no natural per-unit I/O boundary worth isolating. See `docs/GO_NOTES.md`'s "Decomposing the
monolith" section for the full reasoning (this was originally motivated by a real PR review comment
about `SyncWorkloads` risking Temporal's payload-size limit on a large enough source tree).

## Agent Guidelines & Operation

- **Purity discipline**: `internal/transformers`, `internal/manifest`, and `internal/domain` must
  stay free of I/O and Temporal imports. If a change needs either, it belongs in
  `internal/services` or `internal/activities` instead.
- **Testing**: `go test ./...`. Transformers/domain/services are plain unit tests. Workflows use
  `go.temporal.io/sdk/testsuite` with mocked activities (see `internal/workflows/*_test.go`) - no
  real Temporal server needed. A couple of activities tests use real temp directories for the
  trickier extract/transform/render path, including a regression test for the int/float
  serialization gotcha above.
- **Dependency versions**: `go.temporal.io/sdk`, `go.temporal.io/server`, and `go.temporal.io/api`
  are deliberately pinned to versions known to be mutually compatible (`go mod tidy` alone can drift
  `go.temporal.io/api` ahead of what a given `go.temporal.io/server` release implements, which fails
  to compile with a missing-interface-method error). Don't `go get -u` these three independently.
