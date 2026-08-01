# Workflows and activities

`internal/workflows`, `internal/activities`. The orchestration layer and the I/O
boundary — and where nearly every subtle constraint in this codebase lives.

## The split

**Workflows decide, activities act.** Workflow code is replayed from event
history after any worker restart, so it must be deterministic: no clocks, no
files, no network, no map iteration deciding call order. Anything touching the
world is an activity.

Pure transformation is the exception that makes the codebase readable:
`internal/transformers` and `internal/manifest` do no I/O, so workflow code
calls them inline without an activity round trip.

## The boundary is durable, plaintext storage

Everything crossing the workflow/activity boundary — every activity input,
every activity result, every workflow and child-workflow input — is serialised
by Temporal into event history. In external mode that history is persisted in
Postgres and readable through the Web UI and API, indefinitely.

Two consequences drive most of the unusual structure here.

### Secrets must not cross it

Not "should be encrypted", not "kept to a minimum" — must not cross.

`SyncEnvSecrets` is the clearest case. It extracts from AWS, remaps to the
target environment, injects the Keycloak public key, and writes to 1Password,
all in **one** activity, returning only a count. An earlier version extracted in
the parent workflow and passed the plaintext secrets down into each child — a
sensible-looking optimisation that put every secret value into both the parent's
and every child's history.

The accepted cost: `sync-1p` re-extracts from AWS once per target environment
instead of sharing one extraction. One extra idempotent read-only API call per
environment, in exchange for secrets never being persisted.

`RenderTalosTemplates` follows the same shape — read the Secure Note, flatten,
substitute, write, return counts — because both the note and the rendered files
are real cluster bootstrap material.

**Moving a secret into a narrower activity does not satisfy this rule.** If the
value is *returned*, it is in history. The Keycloak admin password ended up read
inside `RunKeycloakSetup` itself, with the input fields deleted so nothing can
pass it in.

### Bulk data must not cross it either

Same mechanism, different failure: Temporal's default 2 MB payload and 4 MB gRPC
limits, hit at activity completion, before anything is written.

`BuildAppFiles` therefore *writes* the files it builds (with a `DryRun` field so
planning still works) rather than returning them for a separate `WriteFiles`
call. And `sync` fans out per app so no payload ever accumulates the whole
source tree.

### The rule of thumb

If a value is secret or unbounded, it does not cross the boundary. Bundle the
whole read-transform-write pipeline into one activity and return a summary.

## Workflow catalogue

| Workflow | Child | Fan-out unit |
|---|---|---|
| `SyncWorkloadsWorkflow` | `SyncAppWorkflow` | app |
| `Sync1PasswordWorkflow` | `Sync1PasswordEnvWorkflow` | environment |
| `SetupKeycloakWorkflow` | `SetupKeycloakEnvWorkflow` | environment |
| `GenerateArgocdWorkflow` | — | — |
| `RenderTalosWorkflow` | — | — |

### Fan-in strategies differ on purpose

- **`setup-keycloak`** — one environment's failure does not stop the others.
  Each child reports its own error independently. Mirrors the original Ruby's
  per-environment `rescue`.
- **`sync-1p`** — any environment's failure fails the run, but every child is
  started before any is waited on, so all failures are collected via
  `errors.Join` rather than aborting at the first.

The pass/fail contract matches the Ruby original in both cases; what changed is
that failures are now reported together instead of stopping at the first.

If you are tempted to make these consistent: don't. The difference is the
behavioural contract.

### `GenerateArgocdWorkflow` regenerates the whole file

One `ApplicationSet` with a matrix generator (env list × service list),
regenerated in full every run — not patched. The GitOps repo moved to an
ApplicationSet-owned model, and continuing to emit per-app-per-env `Application`
files would fight it for ownership of the same names.

The trade-off is a real constraint: **any hand-edit to the generated
ApplicationSet is overwritten on the next run**, including migration settings
like `preserveResourcesOnDeletion`. Change them in the Go template.

`metadata.name` is `ProjectName` (e.g. `pmn`), deliberately *not* matching the
generated filename (`pmn-appset.yaml`). It must match the ApplicationSet already
adopted in-cluster; renaming it would create a second, empty ApplicationSet
rather than taking over the existing one.

## Activity catalogue

| Activity | Does | Notes |
|---|---|---|
| `LoadConfig` | Loads config in the worker's environment | Blanks Keycloak credentials |
| `DiscoverApps` | Globs `SOURCE_DIR` for `APP_PATTERN` | |
| `BuildAppFiles` | Extract → transform → render → **write** | Writes internally; returns a count |
| `WriteFiles` | Writes a file set | Used where payloads are small |
| `FetchSamlCredentials` | Keycloak realm public key + SAML descriptor | Tolerates unreachable Keycloak |
| `SyncEnvSecrets` | AWS extract → remap → 1Password write | Secret-bearing; retries disabled |
| `RenderTalosTemplates` | Note read → render → write | Secret-bearing |
| `CheckKeycloakReady` | One readiness probe | Polled via `workflow.Sleep` |
| `RunKeycloakSetup` | Provisions realm, clients, groups, users | Loads admin credentials itself |

### Retries

Activities retry by default, so they should be idempotent. Where they cannot be,
`nonRetryingActivityOptions()` sets `MaximumAttempts: 1`.

`SyncEnvSecrets` is the case: it ends in a 1Password write that is not safe to
repeat. The side effect is that a transient AWS failure during extraction is not
retried either — an accepted trade. Re-run the command.

### Readiness polling uses durable timers

`setup-keycloak` polls with `workflow.Sleep`, not `time.Sleep`. A durable timer
is recorded in event history: if the worker dies mid-wait, a restarted worker
resumes where the wait left off rather than restarting the countdown. The Ruby
original's blocking `sleep 5` loop lost all progress on any crash.

This is the single feature that most concretely justifies a workflow engine here.

## Testing

`go.temporal.io/sdk/testsuite` with mocked activities. No server required, and
`workflow.Sleep` is fast-forwarded through virtual time, so readiness-polling
tests are instant.

```go
env.OnActivity(a.LoadConfig, mock.Anything).
    Return(activities.LoadConfigResult{Config: cfg}, nil)
```

`mockLoadConfig` in `support_test.go` wraps the call every workflow test needs.

Worth knowing: these tests verify orchestration, not the correctness of what
activities do to the outside world. An activity whose argument vector is wrong
passes a mocked workflow test comfortably — see
[services-and-clients.md](services-and-clients.md) for a case where exactly that
happened.
