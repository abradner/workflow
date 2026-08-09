# Architecture

The abstract view: what this tool is, what it talks to, and the design rules
that explain why the code looks the way it does. For package-level detail see
[modules/](modules/README.md); for running it see [OPERATIONS.md](OPERATIONS.md).

## What it is

A batch generator. It reads configuration and a source repository, derives what
a set of Kubernetes environments should look like, and writes that result into
other repositories and systems.

It is not a controller, an operator, or a service. It has no reconciliation
loop, holds no state between runs, and nothing is deployed from it. The actual
convergence onto the cluster is ArgoCD's job, working from what this tool wrote.

**Rendered files** are pure derivation: same configuration and same source in,
byte-identical output out, regenerated whole every run. That does **not** extend
to the workflows with external side effects. `sync-1p` currently *creates* a
Secure Note rather than updating one, so a second run adds another item with a
new ID; `setup-keycloak` acts against whatever state the target realm is
already in. See [OPERATIONS.md](OPERATIONS.md) for which commands are safe to
re-run.

The unit of work is an **environment** (`dev4`, `dev5`) belonging to a
**project** (`pmn`), containing **apps** (`pmn-ext-gw`, `pmn-core`). Namespaces
follow `<project>-<env>`.

## System context

Three repositories, and this tool sits between them. Getting this relationship
wrong is the single most common source of confusion, because two of the three
are easy to mistake for each other.

```
                    ┌──────────────────────┐
                    │  workflow (this)     │   generator; deployed nowhere
                    └──────────┬───────────┘
             reads             │             writes
      ┌──────────────────┬─────┴──────┬──────────────────┐
      ▼                  ▼            ▼                  ▼
┌───────────┐   ┌────────────────┐  ┌──────────────┐  ┌───────────┐
│  SOURCE   │   │ <project>-     │  │ athena-gitops│  │ 1Password │
│  _DIR     │   │ workloads      │  │ cluster/apps │  │ Keycloak  │
│           │   │ (DEST_DIR)     │  │(CLUSTER_APPS)│  │  Talos    │
│ read-only │   │ manifests      │  │ ApplicationSet│ │ external  │
└───────────┘   └────────────────┘  └──────────────┘  └───────────┘
                         ▲                  │
                         └──────────────────┘
                     ArgoCD reads manifests from
                     the workloads repo, per the
                     ApplicationSet's repoURL/path
```

| Repository | Role | Config |
|---|---|---|
| **workflow** | This tool. A generator, not a deployable. | — |
| **`<project>`-workloads** | Kustomize base/overlay manifests per app. What `sync` writes and what the generated ApplicationSet points at. | `DEST_DIR` (local clone), `REPO_URL` (the URL ArgoCD uses) |
| **athena-gitops** | ArgoCD `ApplicationSet` definitions, auto-synced by a root app-of-apps with `prune: true`. | `CLUSTER_APPS_DIR` |

**`REPO_URL` names the workloads repo, not the GitOps repo**, and `DEST_DIR`
must be a clone of that same repo. Pointing either at `athena-gitops` produces
a configuration that looks plausible and fails at runtime: `sync` commits
overlays into one repository while ArgoCD reads from another, and every
generated Application points at a path that does not exist. This has been
gotten wrong twice.

`prune: true` on the GitOps side raises the stakes of anything written to
`CLUSTER_APPS_DIR`: a file this tool deletes, ArgoCD deletes from the cluster.

## The five workflows

| Command | Workflow | Does | Fans out over |
|---|---|---|---|
| `sync` | `SyncWorkloadsWorkflow` | Discovers apps, extracts base + source overlay, runs the transformer pipeline per target env, writes manifests | app |
| `setup-argo` | `GenerateArgocdWorkflow` | Renders one ArgoCD `ApplicationSet` (matrix: envs × apps) | — |
| `sync-1p` | `Sync1PasswordWorkflow` | Migrates AWS Secrets Manager secrets into 1Password, one Secure Note per env | environment |
| `render-talos` | `RenderTalosWorkflow` | Hydrates Talos bootstrap templates from a 1Password Secure Note | — |
| `setup-keycloak` | `SetupKeycloakWorkflow` | Provisions the Keycloak realm, clients, groups and users per env | environment |

They share configuration and a runtime, and are otherwise independent. There is
no orchestration *between* them; ordering is the operator's business — see
[OPERATIONS.md](OPERATIONS.md).

## Execution model

Temporal supplies the orchestration. The engine is deliberately thin on top of
it: no scheduler, no queue, no state store of its own.

**Workflow code is deterministic and does no I/O.** It decides *what* should
happen. It gets replayed from event history after any worker restart, so it can
never read a clock, a file, or a network socket directly.

**Activities do the I/O.** Anything touching disk, AWS, Keycloak, 1Password or
the network is an activity. Activities may be retried, so they should be
idempotent — and where they cannot be, retries are explicitly disabled.

**Pure transformation sits in between.** `internal/transformers` and
`internal/manifest` are ordinary functions with no I/O, so workflow code can
call them inline without an activity round trip.

### Fan-out

Three of the five workflows decompose into child workflows — `sync` per app,
`sync-1p` and `setup-keycloak` per environment. This buys concurrency, but the
motivating reason was payload size and blast radius: each child's event history
holds only its own unit of work.

Fan-in differs on purpose:

- `setup-keycloak` — **one environment's failure does not stop the others.**
  Each child reports its own error. Mirrors the original's per-environment
  rescue.
- `sync-1p` — **any environment's failure fails the run**, but every child runs
  to completion first, so all failures are reported rather than just the first.

That difference is a deliberate behavioural contract, not an inconsistency.

## Design rules

Four rules explain most of the non-obvious structure. Each was learned from a
concrete failure.

### 1. Secrets must never cross a workflow boundary

Temporal records every activity result and workflow input in durable event
history, readable through the Web UI, the API, and the database. Any secret
crossing that boundary is persisted in plaintext, indefinitely.

So extract, transform and write happen **inside a single activity**, and only
counts come back. `SyncEnvSecrets` does AWS extraction, mapping and the
1Password write in one call. `RenderTalosTemplates` reads the Secure Note,
renders templates and writes files in one call. The Keycloak admin password is
loaded inside the activity that needs it, never returned to workflow code.

The cost is real and accepted: `sync-1p` re-extracts from AWS once per target
environment rather than sharing one extraction, because sharing would mean
passing secret values through the parent.

Moving a secret into a *narrower* activity is not the same as keeping it inside
one. An earlier fix did the former and was correctly rejected.

#### The residue — a known, unfixed limitation

**Keeping secret *values* out of history does not make history safe.** What
still crosses the boundary is metadata, and metadata about secrets is not
nothing:

- environment names and project names
- 1Password item titles (`k8s-<project>-<env>`)
- filesystem paths, app names, counts of secrets and stale fields

Anyone with Web UI, API or database access reads all of it. An attacker who
learns that `k8s-pmn-dev4` exists, holds 19 secrets, and lives in a named
vault has been handed a map.

This is why activity results report **counts, never field names**. Field labels
(`password`, `mp.jwt.verify.publickey`, a username) sit close enough to the
secrets themselves that naming them in history would undo much of the work
above — so the stale-field warning says *how many*, and an operator reads
*which* from the vault.

**What is done about it today:** history retention is set to **one hour**, the
shortest window the server permits (`namespace.MinRetentionLocal`; zero is
rejected as ambiguous with "keep forever", and replicated namespaces are held
to 24h). Every run is also bounded by a one-hour `WorkflowRunTimeout`, since an
unbounded run has no history TTL either. That shortens exposure. **It does not
remove it.**

**What would actually fix it:** a Temporal `DataConverter` with an encrypting
`PayloadCodec`, which encrypts every payload before it reaches history and
decrypts on read. This is Temporal's own sanctioned extension point, configured
at client/worker construction — the right layer, not a workaround. It is not
done because it is a real piece of infrastructure, not a flag: the worker needs
a key at startup, the Web UI needs a codec server to stay readable, key
rotation has to keep old history decryptable, and getting any of that wrong
trades a visible limitation for an invisible one. Tracked separately.

Approaches that do **not** work, so they are not attempted:

- **Hashing or tokenising labels without shared key material.** Field labels are
  drawn from a tiny, guessable vocabulary; an unsalted digest of `password`
  falls to a dictionary in seconds. Salting requires the salt to reach the
  reader, which is key management wearing a disguise.

### 2. Bulk data must not cross a workflow boundary either

Same mechanism, different symptom: Temporal's default 2 MB payload and 4 MB
gRPC limits. Rendered manifests are bulky, so `BuildAppFiles` writes the files
it builds rather than returning them, and `sync` fans out per app so no single
payload accumulates the whole source tree.

### 3. Configuration is loaded by the worker, never the client

`LoadConfig` is an activity. In external mode the CLI and the worker can be
different machines with different filesystems, so a config loaded client-side
serialises the *client's* paths into the workflow — which then resolve to
nothing on the worker. Every workflow's first step is loading config where the
work will actually happen.

### 4. Generated output is regenerated whole, never patched

Every output file is rendered fresh from configuration plus discovery on each
run. Nothing is surgically edited in place.

The consequence is a real constraint: **a hand-edit to a generated file does not
survive the next run.** Changing generated output means changing the template in
this repo. This is most visible in the ArgoCD ApplicationSet, where migration
settings like `preserveResourcesOnDeletion` live in the Go template and must be
flipped there.

The corollary is that this tool does **not** delete files it did not just write.
Cleaning up superseded output is a manual, signed-off step — `CLUSTER_APPS_DIR`
can hold hand-authored files, and the GitOps repo applies it with `prune: true`.

## Run modes

**Embedded** (default) — an in-process Temporal dev server on in-memory SQLite,
plus a worker, for the duration of one command. Zero dependencies. Nothing
survives the process.

**External** — `--temporal=host:port` dials a real server, with a separate
long-lived `worker` process doing the work. Durable across restarts, and the
Web UI can be used to inspect history.

Same workflow code either way. The difference matters for two things: durability
across restarts, and the fact that in external mode event history is *persisted
and readable* — which is what makes rules 1 and 2 above load-bearing rather than
theoretical.

## Where the design came from

The tool is a port of a Ruby ETL CLI with a hand-rolled
`Runner`/`Orchestrator`/`ExecutionContext` framework and an explicit
hydrate → act → commit phase model. Temporal replaced the framework entirely;
`--dry-run` is what survives of the act/commit split.

The Ruby original is preserved verbatim under `ruby-legacy/`. It is outside the
Go module, unaffected by `go build` and `go test`, and kept only so ported
behaviour can be checked against the original intent.
