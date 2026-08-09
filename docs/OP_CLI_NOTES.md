# 1Password CLI behaviour notes

Observed behaviour of the `op` binary this tool shells out to, and the evidence
behind the fake in `internal/serviceclients/op/optest`.

**Verified against:** `op` 2.35.0, macOS, 2026-08-01
**Method:** a throwaway `Testing` vault, dummy values only, `--dry-run` wherever
it would otherwise have written

Re-run this when the CLI is upgraded and correct `optest` for anything that has
drifted. A fake silently out of step with the real binary is worse than no fake,
because it manufactures confidence.

## How stdin is delivered — the thing that matters most

**`op` reads a JSON template from stdin only when stdin is a pipe.** A shell
redirect from a file is ignored — silently, with a zero exit code.

| Form | Template read? |
|---|---|
| `cat item.json \| op item create -` | **yes** |
| `cat item.json \| op item create` (no dash) | **yes** |
| `op item create - < item.json` | **no** — silently ignored |
| `op item create --template=item.json` | yes |

The trailing `-` is documented convention. It is *not* what triggers the stdin
read; being a pipe is. With a pipe the template is read whether or not `-` is
present.

This distinction is invisible at the argument-vector level, which makes it an
easy thing to get badly wrong:

> **This tripped up an investigation of this very file.** Testing by hand with
> `< file.json`, the CLI produced an empty `Untitled SecureNote` and appeared to
> ignore its input entirely. That looked exactly like a bug in this tool's
> `CreateItem`, and a "fix" was written for it. The fix was wrong — the code was
> always correct. Go's `exec.Cmd` with an `io.Reader` stdin gives the child a
> **pipe**, which is the case that works.
>
> If you are reproducing `op` behaviour by hand to compare against what this
> tool does, **pipe it**. A redirect does not reproduce what Go does.

## `op item create`

Working invocation, and the one this tool uses:

```
op item create -            # template on stdin, via a pipe
```

### Category comes from exactly one place

The category may be supplied *either* in the template's `category` field *or*
via `--category`, **never both**:

```
[ERROR] unable to process line 1: cannot provide the item category with both
the JSON template and the `--category` flag - only specify the category in one
location
```

This tool's templates always carry `category`, so `--category` must **not** be
passed. Adding it looks like defensive tidying and breaks a working call. The
fake encodes this so the mistake fails a unit test.

With neither present the CLI errors with
`provide the item category with '--category' flag` — which is what a redirect
produces, since the unread template supplies no category either.

### Without `--vault`, items go to the personal vault

Verified: creating with no `--vault` placed the item in **`Private`**, silently.
Not an error, and rarely what anyone means.

This tool passes no `--vault` today, so `sync-1p` writes its per-environment
Secure Notes into the operator's personal vault rather than a shared tooling
vault. Tracked as part of the `OP_VAULT_NAME` work.

### Field IDs are preserved

Field IDs supplied in the template survive verbatim into the created item. That
is what makes stable field identity across runs possible, and it is the
foundation the read-modify-write upsert work depends on.

## `op item edit`

**Template edit is REPLACE, not merge.** Verified directly: a field omitted from
the template is deleted. In one run `stale_field` was omitted and vanished, a new
field was added with its supplied ID intact, and an updated field kept its ID.

Built-in fields survive omission — `notesPlain` persisted through an edit whose
template never mentioned it. Only custom fields are destroyed.

The consequence for callers: preserving a field you are not modifying means
sending it back verbatim. There is no passive option.

### Settled (2026-08-01)

The dash question is **moot**. `cat f | op item edit <id>` and
`cat f | op item edit <id> -` behave identically - both read stdin, both fail
the same way on a bad payload. The earlier "silent no-op" was caused entirely
by the shell redirect, exactly as on `create`.

**The real constraint is that the payload must be round-tripped.** A
hand-authored partial template is rejected:

```
[ERROR] unable to process line 1: Validation: Couldn't validate the item:
"[ItemValidator] has found 1 errors: {1. Item updatedAt must be > 1970-01-01}"
```

Get the item with `op item get --format json`, mutate it, send the whole thing
back. Verified working, and REPLACE semantics hold on the round-tripped form
too - a dropped field vanished from the preview.

What was **not** established: exactly which metadata fields the validator
requires. Only that omitting them fails, and that the error named `updatedAt`.
`optest` therefore checks for `id` and `updated_at` rather than pretending to
know the full set. Sending the item back whole makes the question moot in
practice.

Consequence for callers: a model that rebuilds the payload from fields it
models produces something the CLI refuses. The Ruby original's `to_h` emitted
only title, category, sections and fields, so its edit path could never have
worked - the same class of defect as its create invocation, and equally
invisible without running the binary.

## `op item get`

- Item-not-found is **exit 1 with a message on stderr**, not empty output on
  exit 0. Callers must distinguish "missing" from "failed" by exit status.
- `--vault` is unnecessary when addressing by item ID — IDs are globally unique,
  and an edit by ID with no `--vault` resolved correctly.
- Getting by *title* without `--vault` resolved during testing, but only because
  the title happened to be unique across the account. Not safe to rely on.
- Secure Notes always carry a built-in `notesPlain` field, present even when the
  creating template never mentioned it.

## Sessions expire mid-run

Every `op` call — including read-only ones — fails with `authorization timeout`
once the desktop session lapses. It lapsed twice during this investigation, once
mid-sequence.

Worth remembering for `sync-1p`, whose 1Password write runs with retries
disabled: a session expiring partway through a multi-environment run leaves some
environments written and others not.

## Testing policy

There is deliberately **no automated test against a real `op` binary or a live
1Password account** — not in CI, and not locally against a throwaway vault. The
blast radius of a test that mutates a real vault is not worth the coverage.

`optest` exists so argv correctness is still checked: it refuses what the real
binary refuses, so a regression fails an ordinary `go test ./...` with no
network, no credentials and no side effects.

It cannot model the pipe-versus-redirect distinction, because that is invisible
at the argv level. Go always provides a pipe, so the fake assumes stdin arrives.
That gap is precisely where the mistake above happened, which is why it is called
out at the top of both this file and the package doc.
