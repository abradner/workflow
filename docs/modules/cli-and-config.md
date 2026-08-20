# CLI, configuration and runtime

`cmd/workflow`, `internal/config`, `internal/temporalutil`, `internal/logging`.

## `cmd/workflow`

Cobra CLI. Five workflow commands plus `worker`.

| File | Holds |
|---|---|
| `main.go` | entry point |
| `root.go` | root command, shared flags, logger construction |
| `commands.go` | the five workflow subcommands |
| `run.go` | the generic `runWorkflow` helper |
| `worker.go` | the long-lived worker subcommand |

Every subcommand is a thin shell over one generic function:

```go
func runWorkflow[TIn, TOut any](
    ctx context.Context,
    opts *globalOptions,
    workflowFn func(workflow.Context, TIn) (TOut, error),
    input TIn,
) error
```

It picks the run mode from `--temporal`, dispatches to `temporalutil.RunEmbedded`
or `RunExternal`, and logs the result. Adding a command means writing the
workflow and a few lines of Cobra wiring.

Note what the input does **not** contain: configuration. `runWorkflow` takes a
plain input value, and no command calls `config.Load()`. See below.

## `internal/config`

One `Config` struct, populated from the environment by `caarlos0/env` struct
tags, with `godotenv` loading a `.env` file first if present.

```go
SourceDir    string   `env:"SOURCE_DIR,required"`
Environments []string `env:"TARGET_ENVS,required" envSeparator:","`
```

`required` means a missing variable fails at load with the variable named.
Directory paths run through `expandPath`, which resolves `~` and relative
segments to absolute — mirroring Ruby's `File.expand_path`.

### Config is loaded by the worker, never the client

**`LoadConfig` is a Temporal activity, and every workflow calls it as its first
step.** This looks like indirection and is not.

In external mode the CLI and the worker are different processes, potentially on
different machines with different filesystems — which is exactly the case under
`docker compose`, where the worker sees `/data/source` and your shell sees
something else entirely. A config loaded client-side and passed in as workflow
input serialises the *client's* paths into the workflow, and the worker then
resolves them against a filesystem where they mean nothing.

Consequences worth internalising:

- **No *top-level* workflow input carries a `Config`** — that is the case that
  reintroduces the bug, because a top-level input is populated by the CLI
  client. A parent passing its own worker-loaded `Config` down to a child is a
  different thing and is fine: `SyncAppInput.Config` and
  `SetupKeycloakEnvInput.Config` both do exactly that. The rule is about *where
  the value was loaded*, not about the field existing.
- Relative paths resolve against the **worker's** working directory.
- In external mode only the worker needs `.env`.

### Secrets are not returned wholesale

`LoadConfig` blanks the Keycloak admin credentials before returning. They are
loaded separately, inside the single activity that uses them.

The reason is the boundary rule: a `Config` returned from an activity is
recorded in event history, so returning one containing a password would persist
that password in plaintext for *every* workflow — including the four that never
touch Keycloak.

An intermediate fix moved the credentials to a dedicated `LoadKeycloakCredentials`
activity. That was still wrong: a narrower activity still *returns* the value to
workflow code. The credentials are now read inside `RunKeycloakSetup` itself, and
the fields were removed from its input struct so nothing can pass them in — a
compile-time guarantee rather than a convention.

`ExternalSecretsAPIVersion` is tagged `env:"-"`: a constant that lives here for
easy upgrading, not a setting.

## `temporalutil` (top-level, exported)

Platform code: wires the two run modes around a consumer-defined
`Engine{TaskQueue, Register}` — this repo's own Engine lives in
`cmd/workflow/engine.go`, whose `registerAll` registers every workflow and
activity, so any worker can serve any command. Also home to the platform's
activity-call conventions: `DefaultActivityOptions`, `NonRetryingActivityOptions`
and `RunActivity` (wrapped privately by `internal/workflows/support.go`).

**`embedded.Run`** (subpackage `temporalutil/embedded`) starts an in-process
Temporal dev server on in-memory SQLite, plus a worker, runs one workflow, and
tears it all down. No external dependencies; nothing survives the process. It is
a subpackage so that importing core `temporalutil` never links
`go.temporal.io/server` into the build.

**`RunExternal`** dials an existing server and waits for the result. It starts
no worker — a separate long-lived worker process (**`RunWorker`**, the `worker`
subcommand) must already be polling the engine's task queue. If nothing is
polling, the workflow is accepted and simply never progresses, which presents as
a hang rather than an error.

The dependency triple `go.temporal.io/sdk`, `.../server` and `.../api` must stay
mutually compatible. `go mod tidy` alone can drift `api` ahead of what `server`
was built against, producing a missing-interface-method compile error. Bump them
together, explicitly.

## `internal/logging`

`slog` wrapper providing the colourised console output the Ruby tool had.
`--verbose` selects debug level. `log.NewStructuredLogger` adapts it to the
interface Temporal's client expects, so engine and workflow logs interleave
coherently.
