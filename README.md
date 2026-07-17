# Workflow

A Go + [Temporal](https://temporal.io) ETL pipeline for migrating, transforming and adapting Kubernetes/GitOps things.

This is a from-scratch Go rebuild of an earlier Ruby version of the same tool: same jobs, same
`.env`-driven configuration, same output - reimplemented with Temporal doing the orchestration
instead of a hand-rolled Runner/Orchestrator framework. The original Ruby implementation is kept
in full under [`ruby-legacy/`](ruby-legacy/) for side-by-side reference - it's not part of the Go
module or build, just a frozen snapshot for comparison. See the
[Ruby → Go File Map](AI_ONBOARDING.md#ruby--go-file-map) in `AI_ONBOARDING.md` for exactly which
Go file replaces which Ruby file.

There are five workflows, each its own CLI command:

### `sync` → SyncWorkloads
**What:** Synchronizes Kustomize manifests for all workloads.
**How:** Discovers apps, extracts `base/` + the source environment's overlay, runs a transformer
pipeline (clone into every target env → modernize legacy Ingress/ExternalSecret shapes → inject a
registry pull secret → link known external services to cluster-local DNS), and writes the result
to the destination directory.

### `setup-argo` → GenerateArgocd
**What:** Generates the core App-of-Apps ArgoCD `Application` manifests.
**How:** One manifest per app × environment, mapped to `<project>-workloads/<app>/overlay/<env>`.

### `sync-1p` → Sync1Password
**What:** Migrates secrets from AWS Secrets Manager to 1Password.
**How:** Extracts secrets from AWS for the source environment, remaps them onto every target
environment (refreshing the Keycloak SAML public key where one is reachable), and provisions one
1Password Secure Note per environment.

### `render-talos` → RenderTalos
**What:** Hydrates Talos cluster bootstrap templates.
**How:** Reads a 1Password Secure Note containing `secrets.yaml` content, flattens it to
dot-notation keys, and substitutes `{{ dotted.key }}` placeholders in every `*.template.yaml` file.

### `setup-keycloak` → SetupKeycloak
**What:** Provisions Keycloak for every target environment.
**How:** Waits for Keycloak to come up, then creates the `neons` realm, its OIDC/SAML clients,
three groups, and three seed users - and writes out the exported SAML descriptor.

## Architecture

Every workflow lives in `internal/workflows/` as a plain Temporal workflow function - the direct
replacement for a Ruby `Workflow::Orchestrator`. There's no generic `Runner`/`needs`-predicate
framework to hand-roll here: each workflow just calls whatever [activities](internal/activities)
it needs, in order, and Temporal's workflow/activity split already gives the same
extract → transform → commit discipline the Ruby version enforced by convention:

- **Activities** (`internal/activities`) are the only place real I/O happens: the filesystem, AWS,
  1Password, Keycloak. Everything here can fail, retry, and is what a Temporal worker actually
  executes.
- **Workflows** (`internal/workflows`) orchestrate activity calls and - where the data involved is
  safe to (see the comment on `BuildAppFiles`) - run pure logic directly, with no I/O of their own.
- **Transformers** (`internal/transformers`) are the pure manifest-mutation pipeline: same
  responsibilities as the Ruby `Workflow::Transformers::*` classes, fully unit-testable with no
  Temporal or filesystem involvement at all.
- **Domain / manifest** (`internal/domain`, `internal/manifest`) are small framework-free types and
  helpers shared across the above.

See `docs/GO_NOTES.md` for a from-first-principles walkthrough of the Go and Temporal concepts this
codebase leans on.

## Usage

Build the binary once:

```bash
go build -o workflow ./cmd/workflow
```

Then drive it exactly like the original CLI:

```bash
./workflow sync [--dry-run] [-v]
./workflow setup-argo
./workflow sync-1p
./workflow render-talos
./workflow setup-keycloak
```

### Two ways to run Temporal

Every command talks to Temporal via `--temporal`, which defaults to `embedded`:

- **`--temporal=embedded`** (default): the command starts an in-process, in-memory Temporal dev
  server, runs a worker inside the same process, executes the workflow, waits for the result, and
  tears everything down - zero external dependencies, for quick local runs.
- **`--temporal=<host:port>`**: dials an existing Temporal server instead (e.g. one started by
  `docker-compose.yml`). A separate long-lived worker must already be polling that server:

  ```bash
  docker compose up -d          # Temporal server + Postgres + Web UI + a `workflow worker`
  ./workflow sync --temporal=localhost:7233
  open http://localhost:8080     # Temporal Web UI
  ```

  Use this mode for anything you want durable across restarts or visible in the Web UI - in
  particular `setup-keycloak`, which polls for readiness for up to a minute using a durable
  Temporal timer rather than blocking the process.

## Configuration

Same as before - a `.env` file at the repo root (see `.env.example`), driven entirely by
project-declaration environment variables. Nothing here changed from the Ruby version's
`config/config.rb`.

## Testing

```bash
go test ./...
```

Workflows are tested with `go.temporal.io/sdk/testsuite` (mocked activities, no server needed);
transformers, domain types, and services are plain `go test` unit tests; a couple of activities
tests exercise real temp-directory I/O for the trickier extract/transform/render path.
