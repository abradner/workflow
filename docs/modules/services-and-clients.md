# Services and service clients

`internal/services/*`, `internal/serviceclients/*`. Everything that talks to the
outside world.

Two layers, deliberately separated:

- **Service clients** — thin wrappers over an external CLI or API. Argument
  vectors, HTTP calls, response parsing. Minimal logic.
- **Services** — business logic per capability, depending on a small interface
  the *service* declares, so the real client can be swapped for a fake in tests.

## Service clients

### `serviceclients/op` — 1Password CLI

Shells out to `op`. The official Go SDK targets read-only Secrets Automation and
cannot create arbitrary multi-section Secure Notes, so the CLI stays.

**This package has caused the most production-affecting bugs in the codebase,
and the reason generalises.** Read [OP_CLI_NOTES.md](../OP_CLI_NOTES.md) before
changing it.

The `Runner` interface exists so tests need no real binary:

```go
type Runner interface {
    Run(ctx context.Context, name string, args []string, stdin []byte) (stdout, stderr string, err error)
}
```

Verified CLI behaviour that the code must respect:

- **`--category` is mandatory on create.** The CLI does not read the `category`
  field from the piped template; without the flag it errors and creates nothing.
- **The trailing `-` is what makes it read stdin**, and it must come after the
  flags. With `-` first, the CLI exits 0 and silently creates an empty
  `Untitled` item.
- **`op item edit <id> -` is a silent no-op** — exit 0, item version increments,
  nothing changes.
- **Template edit is REPLACE, not merge.** A field omitted from the template is
  deleted. Preserving a field you are not modifying means sending it back
  verbatim; there is no passive option. Built-in fields like `notesPlain`
  survive omission.
- **Not-found is exit 1 with stderr**, not empty output on exit 0.
- **`--vault` is unnecessary when addressing by item ID** (IDs are globally
  unique) but required for `create`, or the item lands in the default vault.

Templates go over **stdin, not `--template=<file>`**, because they carry every
extracted secret value and a file would mean writing all of them to disk in
cleartext.

#### Why a stub fake is not enough

The original tests asserted only that `CreateItem` produced
`[]string{"item", "create", "-"}`, against a stub returning whatever the test
wanted. They passed for the entire life of an invocation the CLI rejects
outright — `sync-1p` could never have written to 1Password.

A fake that answers "yes" to any argument vector cannot catch an
argument-vector bug. `internal/serviceclients/op/optest` therefore models the
observed contract and **refuses what the real binary refuses**, so a wrong argv
fails an ordinary `go test ./...` with no network, no credentials, and nothing
mutated.

There is deliberately **no automated test against a real `op` binary or a live
account** — not in CI, not locally against a throwaway vault. The blast radius
is not worth the coverage. The cost of that choice is that `optest` is only as
good as the observations behind it, which is why `OP_CLI_NOTES.md` records a
version and a date: re-verify by hand when the CLI is upgraded.

### `serviceclients/keycloak` — Keycloak REST API

`net/http` against the admin API. Authenticates with `admin-cli`, caches the
bearer token. Provisioning calls treat **409 Conflict as success**, which is
what makes `setup-keycloak` re-runnable — creating a realm that already exists
is not an error for this tool's purposes.

`Ready()` probes the well-known OIDC configuration endpoint with short timeouts
and returns a bool rather than an error: "not up yet" is an expected state
during polling, not a failure.

### AWS

No hand-written client. `services/awssecrets` uses the AWS SDK v2 directly with
the ambient credential chain, rather than shelling out to the `aws` CLI as the
Ruby original did.

## Services

| Package | Responsibility |
|---|---|
| `awssecrets` | Lists and fetches AWS Secrets Manager secrets for an environment |
| `onepassword` | Builds the per-environment Secure Note payload |
| `keycloaksetup` | Provisions realm, OIDC/SAML clients, groups, seed users |
| `discoversamlcreds` | Fetches realm public key + SAML descriptor; nil on unreachable |
| `templaterendering` | Flattens YAML to dotted keys, substitutes `{{ key }}` placeholders |
| `filesystem` | **The only place raw disk I/O happens** |
| `workspaceextractor` | Loads base + source overlay into a `manifest.Workspace` |
| `endpointmapper` | Maps legacy external hostnames onto cluster-local service names |

### `awssecrets`

`ExtractSecrets` filters on `env` and `dev/env`, mirroring the original's CLI
filter, then fetches each value.

**Pagination is explicit.** `ListSecrets` is paginated by the API, and unlike the
`aws` CLI — which paginates transparently — the SDK is not. `NewListSecretsPaginator`
walks every page. Without it an environment with more secrets than fit on one
page silently loses the rest, which looks like success.

Binary payloads are re-encoded to base64 so downstream consumers see the same
string form the CLI-based original produced.

### `onepassword`

Builds one Secure Note per environment: a section per source secret, with JSON
object payloads spread into individual fields and opaque string/binary payloads
under a single `password` field.

Two subtleties, both regression-tested:

- **`parseFlatJSONObject` preserves key order** by decoding token-by-token. Go's
  map decoding randomises iteration order on purpose, which would reshuffle
  fields on every run and produce meaningless diffs.
- **`stringify` maps `nil` to `""`.** `fmt.Sprint(nil)` renders `<nil>`, which
  would write that literal string into a real secret field. Ruby's `value.to_s`
  produced an empty string, and consumers depend on that.

It also uses `UseNumber()` — see the JSON number trap in
[manifest-and-transformers.md](manifest-and-transformers.md).

### `filesystem`

All disk I/O funnels through here: listing, reading, writing, YAML decode. Kept
in one place so everything else stays pure and testable.

Carries the multi-document YAML limitation described in
[manifest-and-transformers.md](manifest-and-transformers.md).

### `keycloaksetup`

Provisions the `neons` realm: an OIDC client and a SAML client, three groups,
three seed users assigned to them. Exports the SAML descriptor as raw XML and
base64.

Loads its admin credentials **itself**, inside the activity, rather than
receiving them — see [workflows-and-activities.md](workflows-and-activities.md)
for why that placement is load-bearing rather than incidental.

## Testing pattern

Every service declares the narrow interface it needs and takes it via a
constructor, so tests inject a hand-written fake:

```go
type Client interface {
    CreateItem(ctx context.Context, item map[string]any) (string, error)
}

func New(projectName string, client Client) *Service
func NewWithClient(client Client) *Service   // tests
```

No mocking framework. Go interfaces are structural, so a five-line fake struct
satisfies them with nothing to declare.

The `op` case above is the caveat worth carrying into any new client: a fake
that accepts every input tests only that your code ran, not that it was correct.
Where an external contract is strict, the fake should be strict too.
