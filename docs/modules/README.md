# Module reference

Package-by-package detail for people changing the code. The abstract view is in
[ARCHITECTURE.md](../ARCHITECTURE.md); the Go and Temporal concepts underneath
are explained in [GO_NOTES.md](../GO_NOTES.md).

| Document | Covers |
|---|---|
| [cli-and-config.md](cli-and-config.md) | `cmd/workflow`, `internal/config`, `internal/temporalutil`, `internal/logging` |
| [workflows-and-activities.md](workflows-and-activities.md) | `internal/workflows`, `internal/activities` |
| [manifest-and-transformers.md](manifest-and-transformers.md) | `internal/manifest`, `internal/transformers`, `internal/domain` |
| [services-and-clients.md](services-and-clients.md) | `internal/services/*`, `internal/serviceclients/*` |

## Layering

Dependencies point downward only. Nothing below reaches up.

```
cmd/workflow            CLI parsing, run-mode selection
    │
internal/workflows      orchestration; deterministic, no I/O
    │
internal/activities     the I/O boundary; the only layer allowed to touch the world
    │
internal/services       business logic per capability
    │
internal/serviceclients thin wrappers over external CLIs and APIs

internal/manifest, internal/transformers, internal/domain
    └── pure data + pure functions; usable from any layer
```

The purity of `manifest`, `transformers` and `domain` is load-bearing rather
than aesthetic: it is what lets workflow code call them inline instead of paying
for an activity round trip.

## Conventions that hold everywhere

**Small interfaces, declared by the consumer.** A package that needs a
dependency declares the minimal interface it uses, next to where it uses it —
`onepassword.Client` needs one method even though the real client has several.
Go interfaces are structural, so nothing has to declare that it implements them.
This is what makes testing possible without a mocking framework.

**Errors are wrapped with context and returned.** `fmt.Errorf("...: %w", err)`
at each level, so a failure reads as a chain. Nothing panics as flow control.

**Table-driven tests, hand-written fakes.** No mocking framework. Workflow tests
use Temporal's `testsuite` with mocked activities.

**Doc comments carry the reasoning.** Several non-obvious decisions are recorded
on the declaration they affect rather than in a document, because that is where
someone about to change them will look. Long doc comments explaining *why* are
deliberate — read them before "simplifying" the thing they describe.

## Where the traps are

Concentrated in three places, all documented in detail in the relevant module
doc:

1. **The workflow/activity boundary** — anything crossing it is serialised into
   durable event history. Secrets and bulk data must not cross.
   → [workflows-and-activities.md](workflows-and-activities.md)
2. **JSON number handling** — decoding into `any` turns every number into a
   `float64`, which silently corrupts large integers and Kubernetes port
   numbers. → [manifest-and-transformers.md](manifest-and-transformers.md),
   [services-and-clients.md](services-and-clients.md)
3. **The 1Password CLI** — argument-order sensitive, with failure modes that
   exit 0. → [services-and-clients.md](services-and-clients.md) and
   [OP_CLI_NOTES.md](../OP_CLI_NOTES.md)
