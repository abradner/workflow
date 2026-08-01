# Manifests, transformers and domain types

`internal/manifest`, `internal/transformers`, `internal/domain`. The pure core:
no I/O, no globals, no clock. Everything here is a function of its arguments.

That purity is a design constraint, not a coincidence. It is what lets workflow
code call these directly instead of wrapping each step in an activity, and what
makes them trivially testable.

## `internal/manifest`

The in-memory model of one application's Kubernetes/Kustomize files, plus the
helpers the transformer pipeline uses to read and mutate it. Nothing here
touches disk — that is `internal/services/filesystem`.

A `Workspace` maps virtual paths (`base/deployment.yaml`,
`overlay/dev4/kustomization.yaml`) to decoded YAML documents. Transformers add,
remove and rewrite entries; rendering happens once, at the end.

| Helper | Purpose |
|---|---|
| `Dig(doc, keys...)` | Walk nested maps, `nil` on any missing or non-map link — Ruby's `Hash#dig` |
| `ExtractEnv(path)` | `overlay/dev4/secrets.yaml` → `dev4` |
| `RenderYAML(doc)` | Encode back to YAML |

### Documents are `map[string]any`

Deliberate. These manifests span dozens of Kubernetes kinds, most of which this
tool only needs to touch one or two fields of. Typed structs for all of them
would be enormous, and — worse — round-tripping through a struct *silently drops
every field the struct does not declare*, quietly deleting parts of the user's
manifest the tool never intended to touch.

The cost is that access is untyped, hence `Dig` and the type assertions
throughout the transformers.

### YAML documents render `---` only for streams

`RenderYAML` prepends `---` separators only for a `[]any` document stream, which
it detects by at least one element having a `kind` key. A single
`map[string]any` renders without a leading separator.

**Known limitation, shared with the Ruby original:** reading a multi-document
YAML file decodes only the first document. A source file containing two Services
is rewritten with only the first. Neither implementation supported this and no
test exercised it, so it is a parity-preserving limitation rather than a
regression — but it is a real one, and worth knowing before assuming a
multi-doc source file survives a `sync`.

## `internal/transformers`

Each takes a `*manifest.Workspace` and mutates it. Composable, order-sensitive.

| Transformer | Does |
|---|---|
| `EnvironmentGenerator` | Deep-clones the source overlay into one per target env. **Must run first** |
| `LegacyModernizer` | Ingress → HTTPRoute, ExternalSecret API upgrades, strips conflicting fields |
| `ServiceAbstractionLinker` | Rewrites external hostnames in configMap literals to cluster-local DNS; generates `ExternalName` Services |
| `PullSecretInjector` | Synthesises a registry pull-secret `ExternalSecret`, wires it into ServiceAccounts |
| `OnePasswordSamlKeyInjector` | Injects a fresh Keycloak public key into JSON secret payloads carrying `mp.jwt.verify.publickey` — **the exception; see below** |

**`OnePasswordSamlKeyInjector` is the exception in this package.** It lives here
by neighbourhood, not by shape: it takes and returns `[]domain.ExtractedSecret`,
never touches a `*manifest.Workspace`, and runs inside the `SyncEnvSecrets`
activity rather than the workload pipeline — so none of the ordering constraints
above apply to it. It injects a fresh Keycloak public key into any JSON secret
payload carrying `mp.jwt.verify.publickey`.

`EnvironmentGenerator` running first is not stylistic: everything after it
operates per target environment, and there are no target overlays to operate on
until it has cloned them.

### The JSON number trap

The most expensive class of bug in this codebase, hit more than once.

Go's `encoding/json` decodes every number into `float64` when the destination is
`any`. It does not remember that the source was an integer. Round-trip a
manifest through JSON and a Kubernetes port `80` becomes `80.0`; an account ID
above 2^53 is silently rounded or re-rendered in scientific notation.

Two defences:

1. **Decode with `json.Number`.** `dec.UseNumber()` preserves the original digit
   string exactly, and `json.Number` implements `fmt.Stringer` so it formats
   verbatim. Used in `OnePasswordSamlKeyInjector.injectPublicKey`, so that
   patching the public key does not corrupt an unrelated numeric field sharing
   the payload.
2. **Do not round-trip at all.** Keeping build-transform-render inside one
   activity avoids the boundary that would have serialised port numbers through
   JSON in the first place — the original motivation for bundling `BuildAppFiles`.

Any new code decoding JSON into `any` needs `UseNumber()`. This is not
hypothetical; it has corrupted output before.

## `internal/domain`

Typed models for the few structures complex enough to earn one — the counterpoint
to `map[string]any`. Extract a domain type when transformers start building
deeply nested maps inline.

| Type | Purpose |
|---|---|
| `kubernetes.ExternalSecret` | Well-formed ExternalSecret manifests |
| `kubernetes.HTTPRoute` | Gateway API HTTPRoute, including Ingress conversion |
| `SamlCredentials` | SAML key/descriptor value object; `PEMPublicKey()` wraps a bare key in PEM armour |
| `ExtractedSecret` | One AWS secret: `Name`, and `String`/`Binary` as pointers |

`ExtractedSecret` uses pointers deliberately: absent and empty are different
states, and AWS distinguishes a secret with no string payload from one whose
string payload is `""`.

## Testing

Table-driven, no mocks needed — pure functions with no dependencies to inject.

Two regression tests are worth preserving deliberately, because both encode bugs
that shipped:

- A 19-digit integer surviving a JSON round trip byte-for-byte.
- A `{"foo":null}` payload rendering as an empty string rather than the literal
  `<nil>` that `fmt.Sprint` produces for a nil `any` — which would otherwise
  write a bogus, non-empty value into a real secret field.
