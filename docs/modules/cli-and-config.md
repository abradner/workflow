# CLI, configuration and runtime

`cmd/workflow`, `internal/config`, and the platform packages `cli`, `temporalutil`,
`configload`, `logging`.

## `cmd/workflow` and the platform `cli/` package

`cmd/workflow` is deliberately thin - the lightweight-consumer shape every tool
built on this platform should have:

| File | Holds |
|---|---|
| `main.go` | names the app (`cli.App{Name, Short, Engine}`) and lists the subcommand factories |
| `engine.go` | the tool's Temporal surface: task queue + `registerAll` (every workflow and activity) |
| `commands.go` | one factory per subcommand; each `RunE` is a one-line `cli.Run(...)` call |

Everything generic lives in the exported `cli/` package: the global
`--dry-run`/`--verbose`/`--temporal` flags, the always-added `worker`
subcommand, and `cli.Run[TIn, TOut]` - the generic start-a-workflow-and-wait
wrapper. It picks the run mode from `--temporal`, dispatches to `embedded.Run`
or `temporalutil.RunExternal`, and logs the result. Adding a command means
writing the workflow and a few lines of factory wiring.

Flag values are bound by cobra between construction and `RunE`, so factories
must read `Options` fields only inside `RunE` - reading at construction time
freezes the defaults.

Note what `cli.Run`'s input does **not** contain: configuration. It takes a
plain input value, and no command calls `config.Load()`. See below.

## `internal/config`

One `Config` struct, populated via the platform's `configload.Load[Config]`
(`caarlos0/env` struct tags, with `godotenv` loading a `.env` file first if
present - resolved against the worker's working directory), followed by this
tool's own post-processing.

```go
SourceDir    string   `env:"SOURCE_DIR,required"`
Environments []string `env:"TARGET_ENVS,required" envSeparator:","`
```

`required` means a missing variable fails at load with the variable named.
Directory paths run through the platform's `configload.ExpandPath` (bare `~` and `~/`
expand; `~user` does not; an empty value resolves to the worker's cwd, since `required`
means set-not-non-empty), which resolves `~` and relative
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

## `logging` (top-level, exported)

`slog` wrapper providing the colourised console output the Ruby tool had.
`--verbose` selects debug level. `log.NewStructuredLogger` adapts it to the
interface Temporal's client expects, so engine and workflow logs interleave
coherently.
