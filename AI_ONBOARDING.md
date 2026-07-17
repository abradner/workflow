# Agent Onboarding: Workflow

## Repository Overview

`workflow` is a Go + Temporal ETL pipeline: five workflows that discover, extract, transform, and
commit Kubernetes/GitOps manifests, AWS→1Password secret migrations, Talos template hydration, and
Keycloak provisioning. It's a from-scratch Go rebuild of an earlier Ruby version (see git history
before this rebuild) - same jobs, same `.env` configuration surface, same output; Temporal replaces
the Ruby version's hand-rolled `Runner`/`Orchestrator`/`needs`-predicate framework.

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
