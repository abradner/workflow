# Go + Temporal, from first principles

You know Ruby; this repo is your first real Go. This doc explains the language features and the
Temporal concepts this codebase actually uses, in the order you'd hit them reading the code
top-down, with pointers to real files. It's meant to be read once start-to-end and then kept around
as a reference - not memorized.

## 1. The shape of a Go project

**Modules and packages.** `go.mod` (repo root) declares the module path
(`github.com/abradner/workflow`) and the Go version. Every directory with `.go` files is a
*package* - Go's unit of code organization, roughly Ruby's `module` but mapped 1:1 to a directory.
`cmd/workflow/` is a `package main` (produces a binary); everything under `internal/` is a library
package other Go code in this module can import, but - because it's under a directory literally
named `internal/` - **no other module can import it**. That's a compiler-enforced rule, not a
convention: it's Go's way of saying "this is application code, not a published library," which
matches how this project is organized (Ruby doesn't have an equivalent - every class is importable
by anyone who requires the file).

**No classes.** Go has no `class` keyword. Instead:

```go
type Config struct {
    SourceDir string
    DestDir   string
}

func (c Config) Summary() string { return c.SourceDir + " -> " + c.DestDir }
```

A `struct` is just data (like a Ruby `Struct` or a plain hash with known keys). A `func` with a
receiver in parens - `(c Config)` - is a *method* on that type, but it's declared separately from
the struct, often in the same file, sometimes not. There's no inheritance; composition (embedding
one struct/interface inside another) is how Go shares behavior - see `logging/logger.go`,
where `Logger` embeds `*slog.Logger` and gets all its methods for free.

**Where to look**: `internal/domain/saml_credentials.go` is the smallest complete example - a
struct plus one method, direct translation of Ruby's `Data.define(...) do ... end`.

## 2. Interfaces: structural, not declared

This is the single biggest mental shift from Ruby. In Go, a type satisfies an interface simply by
having the right methods - there's no `implements` keyword, no explicit declaration anywhere.

```go
type Logger interface {
    Info(msg string, keyvals ...any)
}
```

Anything with an `Info(string, ...any)` method satisfies `Logger` automatically - Temporal's own
`log.Logger`, this repo's `*logging.Logger`, or a five-line fake struct in a test. See
`internal/transformers/onepassword_saml_key_injector.go` for the interface declaration and
`internal/workflows/sync1password.go` where `workflow.GetLogger(ctx)` (a Temporal SDK type) is
passed in directly - it was never told about this interface, it just happens to match.

**Why this matters here**: every package that needs to call out to something effectful (a
filesystem, an HTTP client, a CLI tool) declares its own small interface for exactly the methods it
needs, right next to where it's used - e.g. `discoversamlcreds.KeycloakClient` in
`internal/services/discoversamlcreds/service.go` only has two methods, even though the real
`keycloak.Client` has a dozen. That's deliberate: small, local interfaces are what make Go code
testable without a mocking framework - a test just writes a tiny fake struct. Grep for `fake` across
`_test.go` files to see a dozen examples.

## 3. Errors are values, not exceptions

Go has no `raise`/`rescue`. Instead, any function that can fail returns an `error` as its last
return value, and callers check it explicitly:

```go
cfg, err := config.Load()
if err != nil {
    return err // or wrap it, or handle it - your choice, every time
}
```

There's no way to "forget" to handle an error silently the way you can in Ruby - not checking `err`
is a code smell any Go linter flags immediately, because a subsequent line will usually panic on a
nil/zero value. `fmt.Errorf("doing X: %w", err)` *wraps* an error, preserving the original for
`errors.Is`/`errors.As` while adding context - that's why you'll see chains like
`"extracting %s: %w"` throughout `internal/activities/activities.go`: each layer adds one line of
context as the error propagates up, so a failure three calls deep still reads as a breadcrumb trail.

`panic`/`recover` exist but are reserved for programmer errors (a `nil` map write, an out-of-bounds
index) - never for expected failure paths. You won't find one in this codebase's own code.

## 4. `any`, type assertions, and why manifests are `map[string]any`

Go is statically typed, but sometimes you genuinely don't know a value's shape ahead of time - like
a parsed Kubernetes YAML document, which could be any manifest kind with any fields. `any` (an
alias for `interface{}`) is Go's "could be anything" type, and a *type assertion* is how you narrow
it back down:

```go
doc, ok := content.(map[string]any)
if !ok {
    // content wasn't a map - handle that case
}
```

The `, ok` form never panics; it just tells you whether the assertion succeeded. (The one-value form
`doc := content.(map[string]any)` *does* panic on mismatch - only use it when you're certain.) This
pattern is everywhere in `internal/manifest/dig.go` and every file in `internal/transformers/` -
it's the direct equivalent of Ruby's forgiving `hash[:key]&.dig(...)`, just spelled out explicitly
because Go can't infer "this might not be a Hash" the way Ruby can.

**A real gotcha this caused**: `encoding/json` decodes any JSON number into `any` as `float64`,
always - it doesn't remember whether the original was `3` or `3.0`. That's invisible until you
round-trip a parsed-YAML integer (say, a Kubernetes port number) through JSON and back - which is
exactly what happens at a Temporal activity boundary (see §7). Read the doc comment on
`BuildAppFiles` in `internal/activities/activities.go` for how this shaped the whole activity
design, and `internal/activities/activities_test.go`'s `TestBuildAppFiles_PreservesIntegerFields`
for the regression test.

## 5. Maps have no guaranteed order (on purpose)

Ruby's `Hash` preserves insertion order, always. Go's `map` iteration order is **randomized by the
runtime, deliberately**, specifically so nobody's code can accidentally depend on it. Two
consequences you'll see handled explicitly in this codebase:

- Where a stable order actually matters (writing files in a predictable order, dedup-preserving a
  lookup list), the code uses a `[]T` slice instead of a map, or sorts the map's keys first - see
  `manifest.Workspace.SortedPaths()` and `endpointmapper.SuffixMappings` (a slice of pairs, not a
  map, specifically to preserve check order).
- Where order plain doesn't matter (looking a value up by key), a map is exactly right and nobody
  thinks about it again.

## 6. Generics (the `[T any]` syntax)

Go added generics in 1.18. You'll see them in two small, high-leverage helpers:

```go
func runActivity[T any](ctx workflow.Context, activityFn any, args ...any) (T, error) {
    var result T
    err := workflow.ExecuteActivity(ctx, activityFn, args...).Get(ctx, &result)
    return result, err
}
```

`[T any]` means "this function works for any type T, and the compiler will check it's used
consistently" - calling `runActivity[activities.DiscoverAppsResult](...)` fixes T for that call, so
the return value comes back already typed, instead of every call site duplicating the
declare-a-variable-and-call-Get dance. See `internal/workflows/support.go` and
`temporalutil/embedded.go` (`RunEmbedded[TIn, TOut any]`) - the same pattern lets five
different workflows, each with distinct input/output types, share one runner function.

## 7. Temporal: why a workflow is not just a function call

The Ruby version's `Runner` was a hand-rolled state machine: hydrate predicates, then `act_phase`,
then (unless dry-run) `commit_phase`. Temporal is a real workflow engine that gives you that same
shape - and durability, retries, and observability - for free, at the cost of one hard rule:

> **A workflow function must be deterministic.** Given the same inputs and the same sequence of
> activity results, it must make the exact same sequence of decisions every time it runs.

Why? Temporal doesn't keep your workflow's Go process running for its whole lifetime (which could
be seconds or months). Instead, every decision your workflow makes - "call this activity with these
arguments," "sleep for 5 seconds" - is appended to a durable *event history* on the server. If the
worker process restarts mid-workflow, Temporal **replays** your workflow function from the start,
feeding it the recorded activity results instead of actually re-executing them, until it catches up
to where it left off. If your function would make a *different* decision on replay than it did the
first time (because it read the real clock, or iterated a randomized map, or called a random
number), Temporal can't reconcile the new decision with the old history, and the workflow fails with
a non-determinism error.

This is why:

- **Activities are the only place I/O happens.** `internal/activities/` - disk, AWS, 1Password,
  Keycloak. They're not replayed; only their *recorded result* is reused. They can be non-
  deterministic, flaky, slow - whatever real I/O is - because Temporal never asks them to produce
  the same answer twice.
- **Workflow code never ranges over a map to decide what activity to call next** (§5) - that would
  make the *sequence of activity calls itself* non-deterministic.
- **`workflow.Sleep` replaces a blocking `sleep`.** See `internal/workflows/setupkeycloak.go`'s
  `waitForKeycloakReady`: the original Ruby polling loop used a real `sleep 5` - if that process
  died mid-wait, the next run started the 12-attempt countdown from zero. `workflow.Sleep` records a
  durable timer in the event history; if the worker crashes and restarts, replay fast-forwards
  through everything already recorded and resumes waiting for exactly the time remaining. This is
  the single feature that most concretely justifies "use a workflow engine" for this specific
  orchestrator.
- **Logging goes through `workflow.GetLogger(ctx)`, not a plain logger**, because a normal logger
  would print every log line again on every replay. Temporal's version knows to suppress that.

**Activities vs. plain functions**: an activity is any function registered with a worker and called
via `workflow.ExecuteActivity(ctx, fn, input)` from workflow code. It runs in its own goroutine
(possibly on a different machine entirely), gets automatic retries (`internal/workflows/support.go`
sets a default `RetryPolicy` - exponential backoff, 5 attempts), and its input/output cross a
serialization boundary (see §4's JSON gotcha) - which is exactly why `BuildAppFiles` bundles
extract+transform+render into one activity instead of three: fewer boundary crossings for data that
can't survive them cleanly.

## 8. Testing without a real Temporal server

`go.temporal.io/sdk/testsuite` gives you `TestWorkflowEnvironment`, an in-memory simulator: register
a workflow, mock each activity's expected call and return value with `env.OnActivity(...).Return(...)`
(same `mock` package testify uses for everything else), run it, assert on the result. No network, no
server, sub-millisecond. It even fast-forwards virtual time through `workflow.Sleep` calls, so the
Keycloak polling test in `internal/workflows/setupkeycloak_test.go` exercises all 12 real attempts
without your test suite actually waiting 55 seconds. Compare that file to the plain `go test` unit
tests in `internal/transformers/` - same `testing.T`, same `testify/assert`, just a different setup
step for anything that touches a workflow function specifically.

For genuinely wanting a real server locally, `go.temporal.io/server/temporaltest` (used in
`temporalutil/embedded.go`) spins up a full, real Temporal server backed by an in-memory
SQLite database in a few hundred milliseconds - the same engine behind `temporal server start-dev`,
but embeddable directly in a Go program instead of a separate process.

## 9. A couple of "gotchas we actually hit" worth remembering

- **`gopkg.in/yaml.v3` decodes into `map[string]interface{}`**, not `map[interface{}]interface{}`
  (that was the older v2 behavior) - this mattered a lot, since the entire manifest-transformation
  pipeline assumes string keys throughout.
- **Dependency version skew across a multi-module ecosystem is a real, silent risk.** Adding
  `go.temporal.io/server` (for the embedded dev server) pulled in a *different, incompatible*
  version of `go.temporal.io/api` than the one `go.temporal.io/server` was actually built against,
  which failed at compile time with a confusing "missing interface method" error - not at `go get`
  time. The fix was pinning all three related modules (`sdk`, `server`, `api`) to versions verified
  compatible together, and never letting `go get -u` touch them independently. `go mod why` and
  `go mod graph` were the tools that made the actual cause visible.
- **`go vet` and `gofmt -l .`** are both worth running before every commit - `gofmt` on this codebase
  will silently reformat things like struct field alignment; `go vet` catches real bugs (e.g. a
  format-string/argument mismatch) that compile fine but are wrong.

## 10. Decomposing the monolith: child workflows, fan-out/fan-in

The first pass at this port (§7-§9) got the Ruby → Temporal shape right - hydrate, act, commit - but
it still ran each workflow as one big linear function, looping over apps or environments in plain
Go `for` loops and calling activities one at a time. That's a faithful *port*, but it leaves real
Temporal capability on the table. This section covers the second pass: pulling the per-app and
per-environment work out into their own **child workflows**, and why that's a different thing than
"just add a goroutine."

### What a child workflow actually is

A workflow you start from inside another workflow, via `workflow.ExecuteChildWorkflow(ctx, fn,
input)`, is not a function call and not a goroutine - it's a **separate, independent Temporal
workflow execution**, with its own workflow ID, its own event history, its own retry/timeout
options, visible as its own row in the Temporal Web UI. The parent just happens to have started it
and (usually) waits for its result.

```go
futures := make([]workflow.ChildWorkflowFuture, len(apps))
for i, app := range apps {
    futures[i] = workflow.ExecuteChildWorkflow(ctx, SyncAppWorkflow, SyncAppInput{AppName: app})
}
// every child is now running concurrently - none of them waited for a `.Get()` call

var filesWritten int
for i, f := range futures {
    var result SyncAppResult
    if err := f.Get(ctx, &result); err != nil { /* handle */ }
    filesWritten += result.FilesWritten
}
```

`ExecuteChildWorkflow` returns its future immediately without blocking - that's what makes the
**fan-out** loop above start every child before any of them has necessarily finished (or even
started running). The **fan-in** loop right after blocks on each future's `.Get()` in turn, which is
what makes the parent actually wait for all of them. Write both loops, in that order, and you get
real concurrency for free - Temporal schedules however many children the worker's concurrency
settings allow, in parallel, without you managing threads or a worker pool yourself.

### Why this is worth it here specifically

This codebase now has three parent/child pairs: `SyncWorkloadsWorkflow`/`SyncAppWorkflow` (one
child per app), `SetupKeycloakWorkflow`/`SetupKeycloakEnvWorkflow`, and
`Sync1PasswordWorkflow`/`Sync1PasswordEnvWorkflow` (both one child per target environment). Two
different motivations line up behind that split:

1. **Bounding payload size.** This is a real bug a PR reviewer (an automated Codex review, not a
   human) caught on the *first* pass: `SyncWorkloadsWorkflow` used to build every app's rendered
   files into one `allFiles` slice and hand the whole thing to a single final `WriteFiles` activity
   call. Every activity result and every activity call's arguments get recorded into Temporal's
   event history - and Temporal enforces a default 2MB payload / 4MB gRPC message limit on any single
   one of those. A big enough source tree (or just enough apps) would eventually blow that limit and
   fail the whole sync before writing anything. Per-app children fix this structurally, not just by
   coincidence: `SyncAppWorkflow`'s own history only ever holds *one app's* files, because that's all
   that ever crosses an activity boundary inside it. The parent's own history stays tiny - it only
   ever sees small `SyncAppResult{FilesWritten int}` summaries back from each child, never file
   content. No matter how large the whole source tree grows, no single payload anywhere in this
   workflow scales with it - only with one app's share.
2. **Genuine parallelism for genuinely independent work.** Every app's build-and-commit is
   independent of every other app's; every environment's Keycloak setup and every environment's
   1Password sync is independent of every other environment's. The original sequential `for` loops
   processed them one at a time for no real reason - nothing about extracting AWS secrets for
   environment A depends on having finished environment B first. Child workflows turn "independent
   in principle" into "concurrent in practice," and it costs nothing extra to write.

### Two different fan-in strategies, on purpose

`SetupKeycloakWorkflow` and the other two use *different* logic for what to do when a child fails,
and that difference isn't an oversight - it's carried over from what the original Ruby code did:

- **`SetupKeycloakWorkflow`: isolate and continue.** The Ruby original explicitly rescued each
  environment's setup individually (`SetupKeycloak#commit_phase` had a per-environment `rescue`), so
  one broken environment never stopped the others from being provisioned. The child-workflow version
  preserves that exactly: the fan-in loop calls `.Get()` on every future, logs and `continue`s past a
  failure, and only increments `EnvironmentsSucceeded` on success. The workflow as a whole still
  "succeeds" (no error) even if some environments failed - the result struct is how a caller finds
  out which.
- **`SyncWorkloadsWorkflow` and `Sync1PasswordWorkflow`: collect every failure, still fail overall.**
  Neither Ruby original rescued per-unit failures - a broken app or a broken environment failed the
  whole run. That contract is preserved (any single failure still fails the workflow), but *how* the
  failures are collected changed for the better: instead of stopping dead at the first failure (as a
  sequential loop does), every child still gets to run to completion, and every failure is reported
  together via [`errors.Join`](https://pkg.go.dev/errors#Join) (Go 1.20+) - which lets you wrap
  multiple errors into one `error` value that still works with `errors.Is`/`errors.As` against any of
  them:

  ```go
  var errs error
  for i, f := range futures {
      if err := f.Get(ctx, &result); err != nil {
          errs = errors.Join(errs, fmt.Errorf("app %s: %w", apps[i], err))
      }
  }
  if errs != nil {
      return Result{}, errs // reports every failing app/environment, not just the first
  }
  ```

  This is a strict improvement over the original sequential behavior with no change to the pass/fail
  contract itself: you now find out about *every* broken app in one run instead of fixing one, rerunning,
  discovering the next, rerunning again.

### When *not* to decompose

`GenerateArgocdWorkflow` and `RenderTalosWorkflow` were deliberately left as single linear
workflows. Decomposition isn't free - it's another workflow type to register, another
input/output struct pair, another indirection to read through - so it should earn its keep. Neither
of these does:

- **`GenerateArgocdWorkflow`** has no per-app or per-environment *I/O* to isolate at all - manifest
  generation is pure, in-memory string/YAML building with no activity calls per unit, and the one
  real activity call (`WriteFiles`) handles manifests small enough that payload size was never a
  risk. There's nothing to parallelize and nothing to bound.
- **`RenderTalosWorkflow`** reads one Secure Note and one template directory as a single unit - there
  is no natural "per-something" boundary to fan out over in the first place, and the template set is
  small and fixed in size.

The lesson generalizes past this codebase: reach for a child workflow when you have a genuine
per-unit **payload-size** risk, a genuine per-unit **failure-isolation** need, or genuine per-unit
**independent work** worth running concurrently - not as a default way to structure every workflow.
A workflow with no natural unit to split on gains nothing from being split.

### Testing child workflows

`TestWorkflowEnvironment` (§8) executes a real child workflow inline, using the same mocked
activities as its parent, exactly like a normal (non-child) workflow call - **except** it requires
the child's workflow function to be explicitly registered first with `env.RegisterWorkflow(...)`,
the same call you'd make on a real worker (`cmd/workflow/engine.go`'s `registerAll`). Forget it and the
test panics immediately with `unable to find workflow type: ... Supported types: [...]` naming
exactly what *is* registered - a clear, fast failure, not a silent no-op. If you ever want to stub a
child wholesale instead of letting it run for real (useful for a parent-only test where the child's
own behavior is covered elsewhere), `env.OnWorkflow(ChildFn, mock.Anything).Return(...)` works
exactly like `OnActivity`.

## 11. Temporal's event history is not a secrets vault

A round of follow-up review (after §10's decomposition) caught something more serious than payload
size: several workflows let real secret material cross into workflow code as an activity result or
workflow input. Temporal records every one of those, byte-for-byte, in its durable event history. In
external/durable mode (the docker-compose setup this whole tool supports), that history lives in
Postgres and is browsable through the Temporal Web UI and API - so anyone with read access to either
could read whatever crossed that boundary, just by opening the right workflow execution.

**The fix, every time, was the same shape**: never let the sensitive value leave the one activity
that needs it. Four instances of this got caught and fixed, each a variation on the same idiom §7
already used for `BuildAppFiles`'s int/float problem:

- **AWS secrets** (`Sync1PasswordEnvWorkflow`): used to receive `Secrets []domain.ExtractedSecret` as
  child-workflow input (extracted once by the parent) and separately call an `IngestVaultItem`
  activity with the mapped result - three places those values got recorded. Fixed by bundling
  extraction, mapping, and ingestion into one activity, `SyncEnvSecrets`. Only a final secret *count*
  crosses back into workflow code now. Cost: extraction happens once per target environment instead
  of once shared across all of them (see the doc comment for the full trade-off, including why that
  activity runs with retries disabled and what that means for transient AWS failures).
- **Talos secrets** (`RenderTalosWorkflow`): used to read the Secure Note via one activity, parse and
  render templates in workflow code, then write via a separate `WriteFiles` call - the raw Secure
  Note content and the rendered (secret-bearing) files each crossed a boundary of their own. Fixed by
  bundling read+parse+render+write into one activity, `RenderTalosTemplates`. The workflow itself
  shrank to a single activity call.
- **The Keycloak admin password** - this one took two attempts, which is itself worth remembering.
  The first fix moved `KeycloakAdminPassword` out of `LoadConfig`'s result (shared by all five
  workflows) into a dedicated `LoadKeycloakCredentials` activity called only from
  `SetupKeycloakEnvWorkflow`. That solved "every workflow's history has this password" - but a
  second review round correctly pointed out it didn't solve "this one workflow's history has this
  password": `LoadKeycloakCredentials`'s result still crossed back into workflow code, then back out
  again as `RunKeycloakSetup`'s own input. **Moving a secret to a narrower activity isn't the same as
  keeping it inside one.** The actual fix: delete `LoadKeycloakCredentials` entirely, and have
  `RunKeycloakSetup` call `config.Load()` itself, inline, for the one field it needs. The password
  now only ever exists inside that single activity invocation.
- **A single large app's manifests** (`SyncAppWorkflow`, P2 rather than P1 - a size risk, not a
  secrecy one, but the same fix): fanning out to one child workflow per app (§10) bounded the
  *aggregate* payload risk across a whole source tree, but `BuildAppFiles`'s result and a separate
  `WriteFiles` call's input still both carried one app's full rendered content - so one
  unusually-large single app could still trip Temporal's limits within its own child. Fixed by
  folding the write into `BuildAppFiles` itself (with a `DryRun` field to still support a
  plan-only run), the same way `RenderTalosTemplates` does.

**The general principle**: treat "does this activity result or workflow input contain something
that must stay secret (or unboundedly large)?" as its own design question, separate from
determinism. And when you fix it, check that the fix actually keeps the value inside one activity
call - not just moves it to a smaller one that still hands it back to workflow code in between.

**What this doesn't solve**: Temporal has an official answer for cases where sensitive data
genuinely can't be kept inside one activity call - a [`PayloadCodec`](https://docs.temporal.io/production-deployment/data-encryption)
that encrypts every payload before it's recorded and decrypts it on the way back out, configured on
both the client's and worker's `DataConverter`. This codebase doesn't implement one (every
sensitive value here turned out to be avoidable by restructuring activities instead), but it's the
right tool when restructuring isn't enough - e.g. a workflow that genuinely needs to hold a secret
in its own state across multiple steps, not just pass it once into one activity.
