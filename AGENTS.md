# Agent Onboarding: Workflow

This is the canonical steering file for coding agents working in this repo.
`CLAUDE.md` imports it rather than duplicating it, so there is one set of
instructions rather than two that drift.

## Start here

This file covers layout, the workflow contract, and the working rules. For
anything deeper, go to [docs/](docs/README.md) rather than reverse-engineering
it from the code:

| Question | Read |
|---|---|
| What is this system, and why is it shaped like this? | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) |
| What does this package do, and what will bite me? | [docs/modules/](docs/modules/README.md) |
| How do I run it / why did a run fail? | [docs/OPERATIONS.md](docs/OPERATIONS.md) |
| I need the Go or Temporal concept explained | [docs/GO_NOTES.md](docs/GO_NOTES.md) |
| I'm touching anything that shells out to `op` | [docs/OP_CLI_NOTES.md](docs/OP_CLI_NOTES.md) — **read before changing** |
| How the Ruby original mapped onto this | the Ruby → Go File Map below, and [`ruby-legacy/`](ruby-legacy/) |
| Shipping several PRs as a batch | [`.claude/skills/batch-review/SKILL.md`](.claude/skills/batch-review/SKILL.md) |
| Reviewing local commits before pushing | [`.claude/skills/independent-commit-review/SKILL.md`](.claude/skills/independent-commit-review/SKILL.md) |
| What bit us before | **Gotchas & Lessons Learned**, at the end of this file |

About to push a batch of local commits? Read
[`.claude/skills/independent-commit-review/SKILL.md`](.claude/skills/independent-commit-review/SKILL.md)
first — fresh-eyes review per commit, with every fix proven by watching its test
fail before it passes. Cheaper before the PRs exist than after.

Shipping a body of work as several PRs? Read
[`.claude/skills/batch-review/SKILL.md`](.claude/skills/batch-review/SKILL.md)
first — it is the repo's stacked-PR workflow, and its central rule is that
review feedback is **write-only until the whole batch is synthesised**. If you
are looking at an open PR whose description carries a `## Batch` block, do not
push fixes to it.

Two rules that are easy to violate without noticing, both explained in
`docs/ARCHITECTURE.md`:

- **Nothing secret or unbounded may cross a workflow/activity boundary.**
  Temporal records every activity result and workflow input in durable,
  readable event history. Bundle read-transform-write into one activity and
  return a count.
- **Generated output is regenerated whole, never patched** — and this tool never
  deletes files it did not just write.

## Repository Overview

`workflow` is a Go + Temporal ETL pipeline: five workflows behind six CLI commands (`prune-1p`
reuses the sync-1p workflow with `Prune` set) that discover, extract, transform, and
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

This repo is becoming a reusable **platform**: the exported top-level packages (`cli/`,
`temporalutil/`, `configload/`, `logging/`, `filesystem/`) are consumer-agnostic library code that
other tools import; everything under `internal/` plus `cmd/workflow/` is this tool's own domain —
the platform's first consumer. Platform packages must never import `internal/`.

**Platform packages (exported):**

- **`cli/`**: The command-line harness. A consumer passes an `App{Name, Short, Engine}` and its
  command factories to `cli.New` and gets the global `--dry-run/--verbose/--temporal` flags, the
  `worker` subcommand, and `cli.Run` (the generic start-a-workflow-and-wait wrapper) for free.
- **`temporalutil/`**: Wires up the two run modes (embedded via
  `go.temporal.io/server/temporaltest` in subpackage `temporalutil/embedded` - a subpackage so
  importing the core never links the server, external via `client.Dial`) around a
  consumer-defined `Engine{TaskQueue, Register}`, plus the platform's activity-call conventions
  (`DefaultActivityOptions`, `NonRetryingActivityOptions`, `RunActivity`). This repo's own
  Engine lives in `cmd/workflow/engine.go`.
- **`configload/`**: godotenv + caarlos0/env harness (`Load[T]`, `ExpandPath`) behind each
  consumer's own Config struct. Config loads on the worker, via a LoadConfig activity.
- **`converge/`**: Human-in-the-loop machinery for two-pass workflows - `Question`/`Answer`
  types, the interactive `Prompter`, pre-supplied-answer matching (`Apply`), non-interactive
  `UnresolvedError`, and `SavePlan`/`LoadPlan` with a `GeneratedAt` freshness stamp. CLI-side
  only: workflows never prompt; a decision crosses the boundary as ordinary input/result data.
- **`poll/`**: Durable wait-for-completion loops for workflow code (`Until`: check every
  interval via workflow timers, bounded by a budget, `ErrBudgetExhausted` carrying the last
  known state) - the replacement for a CLI tool's `sleep N` busy-wait.
- **`logging/`**: Colorized console logger for CLI/activity code (never workflow code).
- **`filesystem/`**: Raw disk I/O service - the platform's only place files get touched.

**This tool's own code (the first consumer):**

- **`cmd/workflow/`**: CLI entry point: `main.go` names the app, `engine.go` defines its Temporal
  surface (task queue + workflow/activity registration), `commands.go` defines the six
  subcommands - each a one-line wrapper around `cli.Run`.
- **`internal/config/`**: Env-driven `Config` struct layered on `configload`. One flat struct
  threaded through workflow → activity inputs as needed.
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
- **`internal/services/`**: Business logic built on the service clients - `awssecrets` (AWS SDK
  v2, not a CLI wrapper), `onepassword`, `templaterendering`, `keycloaksetup`,
  `discoversamlcreds`, `endpointmapper`, `workspaceextractor`.
- **`internal/activities/`**: Every Temporal activity - the only place non-determinism/I-O is
  allowed to happen from a workflow's perspective. Methods on `*Activities` so real dependencies
  swap for fakes in tests.
- **`internal/workflows/`**: The five Temporal workflows behind the six CLI commands
  (`prune-1p` reuses `Sync1PasswordWorkflow`), plus their per-unit children. See the package doc
  comment in `internal/workflows/support.go` and each file's doc comment for what it does and why
  it's structured the way it is.

## Ruby → Go File Map

The full pre-rebuild Ruby implementation is kept at [`ruby-legacy/`](ruby-legacy/) for side-by-side
reference (see the README's note on it). This is the file-by-file correspondence.

**CLI entry point**

| Ruby (`ruby-legacy/`) | Go |
| --- | --- |
| `workflow.rb` | `cmd/workflow/main.go`, `engine.go`, `commands.go` (thin wrappers over the platform `cli/` package) |
| `config/config.rb` | `internal/config/config.go` |

**Framework - superseded, not ported 1:1** (Temporal's workflow/activity model replaces the concept
entirely; see "Architecture: Workflow Contract" below)

| Ruby | Go |
| --- | --- |
| `app/workflow/orchestrator.rb` | *(none - `internal/workflows/*.go` are plain functions, no base class)* |
| `app/workflow/runner.rb` | *(none - `temporalutil/` + Temporal itself)* |
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
| `app/services/filesystem_service.rb` | `filesystem/service.go` (top-level, exported) |
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
| `app/utils/colorized_logger.rb` | `logging/logger.go` (top-level, exported) |

**Tests**: each `spec/**/*_spec.rb` has a same-purpose `*_test.go` next to the Go file it covers
(e.g. `spec/services/one_password_service_spec.rb` ↔ `internal/services/onepassword/service_test.go`);
`spec/orchestrators/*_spec.rb` and `spec/workflow_e2e_spec.rb` are covered by
`internal/workflows/*_test.go` using Temporal's `testsuite` package instead of RSpec doubles.

**New in Go, no Ruby counterpart**: `internal/activities/` (the Temporal I/O boundary itself -
Ruby had no equivalent framework concept) and `temporalutil/` (embedded/external Temporal
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

**`LoadConfig` deliberately blanks the Keycloak admin password**: `Config.KeycloakAdmin`/
`KeycloakAdminPassword` are zeroed out before `LoadConfig` returns. Every activity result and
workflow input is recorded in Temporal's durable event history - visible via the Web UI/API/DB in
external mode - regardless of whether anything downstream reads that field, so `LoadConfig`'s result
being shared by all five workflows meant this password used to show up in every one of their
histories, not just Keycloak's. The real value is loaded instead by `RunKeycloakSetup` itself,
inline, via its own `config.Load()` call - not by a separate credentials-loading activity, which
would still hand the password back to workflow code in between (an intermediate fix here that turned
out not to be good enough - see `docs/GO_NOTES.md`'s "Temporal's event history is not a secrets
vault" section for why). Same underlying principle is why `Sync1PasswordEnvWorkflow` bundles AWS
secret extraction, mapping, and 1Password ingestion into one activity (`SyncEnvSecrets`), and why
`RenderTalosWorkflow` bundles its Secure-Note read, template rendering, and write into one activity
(`RenderTalosTemplates`) - real secret values never pass through workflow code at all in any of the
three.

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

**`GenerateArgocd` writes one `ApplicationSet`, not one `Application` file per app × environment**:
this changed because the target GitOps repo (`athena-gitops`) moved `cluster/apps/`'s `pmn` project
from individually-committed `Application` manifests to a single `ApplicationSet` with a matrix
generator (env list × service list) - continuing to write per-app-per-env files would fight that
ApplicationSet for ownership of the same Application names. The whole file is regenerated from
`Config` + `DiscoverApps` on every run, same as everything else this tool outputs - see the doc
comment on `GenerateArgocdWorkflow` for the trade-off that implies (manual edits to the
ApplicationSet's boilerplate, made directly in the GitOps repo, don't survive the next run unless
mirrored back into this Go template).

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
- **Plan before large changes**: for anything moderately or highly complex, propose an
  implementation plan and get it reviewed before executing. Heading down the wrong architectural
  path is far more expensive than the plan. (This repo has already paid for skipping it once - see
  gotcha 6.)
- **Batch work ships via the batch-review workflow**
  ([`.claude/skills/batch-review/SKILL.md`](.claude/skills/batch-review/SKILL.md)): a stack of small
  single-commit PRs, review feedback write-only until synthesis, reactive work in one followup PR.
  - **Scope: this binds author-side agents only.** If you are writing code, fixing CI, or handling
    feedback on a batch PR that is not the followup, a CI event or review comment is a ledger entry,
    not a work order. Record it and stand down. The only interstitial showstopper is irreversible
    loss on merge.
  - **Automated reviewers (Copilot, Codex, etc.) are explicitly NOT in scope**: review every PR
    fully and leave all findings as normal. Your comments are harvested and synthesised into the
    followup; the absence of replies on an interstitial is the workflow operating as designed, not
    your feedback being ignored.
- **Evaluate review feedback on its merits — a finding is a claim, not a
  verdict.** Reviewers, human and automated, are frequently right and
  occasionally confidently wrong. Before acting on one:
  - **Trace it to the code or reproduce it.** Fix only what you can verify.
    Plausible-sounding claims about runtime behaviour are the ones most worth
    checking, because they are the ones that read as obviously true.
  - **Ask whether it is reachable.** A finding can be entirely correct about the
    world and still irrelevant here, because our code cannot produce the input
    it describes. Guarding an unreachable path costs maintenance and buys
    nothing.
  - **Check which end is wrong.** When a comment reports a contradiction between
    code and documentation, the documentation is often the side to fix. Do not
    assume the code is the defect because that is what the comment points at.
  - **Weigh the context the reviewer had.** A bot reviewing one PR cannot see a
    deliberate decision made three PRs earlier, or a constraint recorded in
    `docs/`. Severity badges and assertive phrasing are formatting, not evidence.
  - **Declining has to actually happen.** A round that accepts every finding is a
    warning sign, not a good score — it usually means the review is being
    deferred to rather than assessed. When declining, write the reason down where
    the decision is auditable: the PR body, or the review thread.
  - This applies to **your own conclusions too**, and most sharply there. The
    worst error in this repo's history was self-generated: a confident
    misdiagnosis that produced a "fix" for working code (gotcha 6). One
    verification step would have caught it, and none was run because the
    conclusion felt solid.
- **Keep the module docs current**: [`docs/modules/`](docs/modules/README.md) names real
  identifiers and describes real invariants. When a change moves, renames, or invalidates one,
  update the doc **in the same PR**. A module doc describing a boundary that no longer exists is
  worse than none, because it is trusted.
- **Log discovered work**: bugs, tech debt and follow-ups go in the Notion tracker, not in a PR
  body or a code comment. Tasks data source `c0e96552-0a49-4311-8d51-8f5ad7ae86a8`, linked to the
  project via the `Project` relation; set `Impact` and `Effort level`, and put the finding and
  acceptance criteria in the body. A triage table in a PR body goes stale between rounds and
  vanishes on merge.
- **Destructive and outward-facing actions need explicit approval**, every time - deleting files,
  vault items, branches or remote refs, and anything that writes to a real 1Password vault, AWS, or
  Keycloak. Prepare the change and hand the destructive step to the operator rather than running it.
  Approval for one instance is not approval for the next.
- **Use the structured file tools** (read/edit/write) rather than `cat`, `sed` or shell here-docs
  for reading and modifying repo files. Scope: this governs how an agent reads and edits files
  during a session; committed scripts and pipelines processing command output use shell tools
  freely.

## Gotchas & Lessons Learned

Things that have actually bitten, kept as a numbered list so it accretes. Add to it rather than
rewriting it.

1. **JSON numbers decode to `float64`.** Go's `encoding/json` does not remember that a number was
   an integer. Round-trip a manifest and a Kubernetes port `80` becomes `80.0`; an ID above 2^53 is
   rounded or rendered in scientific notation. Decode with `dec.UseNumber()`, or avoid the
   round-trip entirely by keeping build-transform-render inside one activity. Regression-tested with
   a 19-digit integer.

2. **`fmt.Sprint(nil)` renders `<nil>`.** A JSON `null` decoded into `any` is a nil interface, and
   printing it writes the literal string `<nil>` into what may be a real secret field. Ruby's
   `value.to_s` produced an empty string; `stringify` preserves that. Regression-tested.

3. **The Temporal dependency triple must move together.** `go.temporal.io/sdk`, `.../server` and
   `.../api` are pinned mutually compatible. `go mod tidy` alone can drift `api` ahead of what
   `server` implements, failing to compile with a missing-interface-method error. Never
   `go get -u` them independently.

4. **Event history is durable, readable, plaintext storage.** Every activity result and workflow
   input is recorded, and in external mode persisted in Postgres and visible through the Web UI.
   Moving a secret into a *narrower* activity does not help - if it is returned, it is in history.
   Bundle the whole read-transform-write into one activity and return a count.

5. **Multi-document YAML: only the first document is decoded.** A source file containing two
   Services is rewritten with only the first. Shared with the Ruby original and untested there
   either, so it is a parity-preserving limitation rather than a regression - but it is a real
   data-loss path.

6. **When reproducing a shell-out by hand, reproduce how the *program* invokes it.** `op` reads a
   stdin template only when stdin is a pipe; a shell redirect (`< file`) is ignored silently, exit
   0. Hand-testing with a redirect made working code look broken, and a "fix" was written and
   tested before verification caught it - the fix would have broken the working call. Go's
   `exec.Cmd` with an `io.Reader` gives the child a pipe.

7. **`op` sessions expire mid-run**, and then *every* call fails with `authorization timeout`,
   including read-only ones. `sync-1p` runs its vault write with retries disabled, so an expiry
   partway through a multi-environment run leaves some environments written and others not.

8. **`REPO_URL` and `DEST_DIR` name the workloads repo, not the GitOps repo.** Gotten wrong twice.
   Both symptoms surface at ArgoCD sync time, well after the run that caused them reported success:
   `sync` commits overlays where ArgoCD is not looking, and every generated Application points at a
   path that does not exist.

9. **A test that needs a real directory may not be doing file I/O.**
   `TestLoad_ExpandsRelativePaths` uses `t.TempDir()` for two reasons, only one obvious: `os.Chdir`
   needs a directory that exists, *and* chdir'ing somewhere empty stops `godotenv.Load()` picking up
   a developer's real `.env` and overriding the test's environment. Removing the temp dir as
   "unnecessary" would reintroduce a test that passes or fails depending on someone's local file.

10. **Relative paths resolve against the worker's working directory, not yours.** Config is loaded
    by an activity, which runs wherever the worker runs - a container with its own mounts in
    external mode. A path that works from your shell can resolve to nothing on the worker.
