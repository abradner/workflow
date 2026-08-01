# Operator's manual

Running this tool, and working out what happened when a run goes wrong. For
what the system *is*, see [ARCHITECTURE.md](ARCHITECTURE.md).

## Before the first run

### Tools

| Tool | Needed for | Notes |
|---|---|---|
| Go 1.26+ | building | `go.mod` requires 1.26.4; `mise.toml` pins 1.26 |
| `op` (1Password CLI) | `sync-1p`, `render-talos` | must be signed in — see below |
| AWS credentials | `sync-1p` | ambient chain: env, `~/.aws`, SSO, instance role |
| Docker | external mode only | not needed for embedded runs |

`op` uses the ambient session. It expires, and when it does **every `op` call
fails with `authorization timeout`** — including read-only ones. Confirm before
a long run:

```bash
op vault list
```

If that prompts or fails, unlock the desktop app and retry. A session that
expires mid-run leaves partial output; see [Recovery](#recovery).

### Configuration

All settings come from the environment, optionally via a `.env` file. Copy
`.env.example` and fill it in. Everything is required unless marked otherwise —
a missing variable fails at startup with the variable named, which is the
cheapest failure mode available.

| Variable | Meaning |
|---|---|
| `SOURCE_DIR` | Read-only source repo clone |
| `DEST_DIR` | **Clone of `REPO_URL`.** Where `sync` writes manifests |
| `REPO_URL` | The **workloads** repo URL. Goes into the ApplicationSet's `repoURL` |
| `CLUSTER_APPS_DIR` | GitOps repo's `cluster/apps/`. Where `setup-argo` writes |
| `TALOS_TEMPLATE_DIR` | Directory of `*.template.yaml` files |
| `SOURCE_ENV` | Environment to derive *from* |
| `TARGET_ENVS` | Comma-separated environments to derive *to* |
| `PROJECT_NAME` | Namespace prefix: `<project>-<env>` |
| `APP_PATTERN` | Glob for app discovery under `SOURCE_DIR` |
| `TLD` | Domain for generated hostnames |
| `REGISTRY_HOSTNAME`, `REGISTRY_1P_ITEM_ID` | Registry pull secret |
| `OP_TALOS_ITEM_ID` | Secure Note for `render-talos`. **Not** `required` in `config.Config`, despite the rule above: omitting it fails later inside `op` with an empty item ID rather than at startup |
| `KEYCLOAK_ADMIN`, `KEYCLOAK_ADMIN_PASSWORD` | Optional, both default to `admin` |

> **The one that bites.** `REPO_URL` and `DEST_DIR` refer to the **workloads**
> repo, *not* the GitOps repo. Point them at the GitOps repo and `sync` commits
> overlays where ArgoCD is not looking, while every generated Application points
> at a path that does not exist. Both symptoms appear at ArgoCD sync time, well
> after the run that caused them reported success.

Relative paths are resolved against the working directory **of whichever process
loads config** — the worker in external mode, not your shell. Prefer absolute
paths.

## Running

```bash
go build -o workflow ./cmd/workflow
./workflow <command> [--dry-run] [--verbose] [--temporal=<mode>]
```

| Flag | Effect |
|---|---|
| `--dry-run` | Plan only. No files written, no external state changed |
| `--verbose` / `-v` | Debug logging |
| `--temporal` | `embedded` (default) or `host:port` |

### Commands

| Command | Writes to | Idempotent? |
|---|---|---|
| `sync` | `DEST_DIR` | Yes — regenerates whole files |
| `setup-argo` | `CLUSTER_APPS_DIR` | Yes — regenerates the ApplicationSet |
| `sync-1p` | 1Password | **No** — see below |
| `render-talos` | `TALOS_TEMPLATE_DIR` | Yes |
| `setup-keycloak` | Keycloak, `DEST_DIR` | Mostly — creation tolerates 409 Conflict |

**`sync-1p` is the one to be careful with.** It currently creates a Secure Note
per environment rather than updating an existing one, and its 1Password write
runs with retries disabled because that write is not safe to repeat. Read
[OP_CLI_NOTES.md](OP_CLI_NOTES.md) before running it.

### Suggested order for a new environment

Nothing enforces this; the workflows are independent.

**Keycloak is itself one of the generated workloads**, so the ordering is
genuinely two-phase — `setup-keycloak` cannot run until the Keycloak it
provisions is deployed and reachable, which requires `sync`, `setup-argo`, a
push, and an ArgoCD sync to have happened first.

Phase 1 — get the workloads deployed:

1. `sync-1p` — secrets must exist before workloads referencing them start
2. `sync` — write the manifests
3. `setup-argo` — publish the ApplicationSet that points at them
4. commit and push `DEST_DIR`, then `CLUSTER_APPS_DIR`; wait for ArgoCD to sync

Phase 2 — provision against the now-running instance:

5. `setup-keycloak` — the realm, clients, groups and users

> **The ordering trap.** `sync-1p` injects the live Keycloak public key into any
> secret payload carrying `mp.jwt.verify.publickey`, and it fetches that key from
> the target environment's Keycloak. Run in phase 1 — before Keycloak exists —
> that fetch fails, injection is **silently skipped**, and the Secure Note is
> written without it. Because `sync-1p` currently *creates* rather than updates,
> re-running it after phase 2 adds a second item rather than repairing the first.
> Until the upsert work lands, treat the SAML key as something to verify by hand
> after phase 2, or accept that a first-time environment needs `sync-1p` run
> again and the stale item removed manually.

`render-talos` is cluster bootstrap and stands outside this sequence.

**Always `--dry-run` first** against an unfamiliar configuration. It exercises
discovery and the full transformation pipeline, so a wrong `SOURCE_DIR` or a
broken `APP_PATTERN` surfaces there rather than after files are written.

## External mode

Embedded mode is right for one-shot local runs. Use external mode when you want
durability across restarts or the Web UI.

```bash
docker compose up -d          # postgres, temporal (7233), UI (8080), worker
./workflow sync --temporal=localhost:7233
```

| Service | Port |
|---|---|
| Temporal | 7233 |
| Web UI | 8080 |

Two things routinely catch people out:

- **Only the worker needs `.env`.** The CLI is a client; it submits a request
  and waits. Config is loaded worker-side by design.
- **The worker sees container paths.** Compose bind-mounts your host
  directories to fixed `/data/...` targets and overrides the path variables to
  match. Host paths in your shell's `.env` are irrelevant to the worker.

After changing the code, rebuild the worker — a running one keeps the old binary:

```bash
docker compose up -d --build worker
```

## When a run fails

### Reading the failure

Embedded mode gives you the error on stderr and nothing else — history dies with
the process. External mode keeps full event history in the Web UI, which shows
which activity failed, its inputs, its attempts, and the error.

> **Do not paste event history into a ticket or a chat.** In external mode it is
> a durable, readable record. The design keeps secret *values* out of it, but
> paths, environment names, item titles and counts are all there.

### Common failures

| Symptom | Cause | Fix |
|---|---|---|
| `authorization timeout` from any `op` call | 1Password session expired | Unlock the desktop app, re-run |
| Config error naming a variable | Missing/misspelled env var | Set it; check the worker's environment in external mode |
| `DiscoverApps` finds nothing | `SOURCE_DIR` or `APP_PATTERN` wrong | `--dry-run --verbose`; check the path the worker resolved |
| ArgoCD Applications point at missing paths | `REPO_URL`/`DEST_DIR` at the GitOps repo | Repoint both at the workloads repo, re-run `setup-argo` |
| Blob size limit exceeded | An activity payload exceeded ~2 MB | Genuine bug — the work belongs inside one activity |
| Keycloak readiness times out | Keycloak not up at the derived URL | Check the derived hostname; confirm the instance is reachable |
| Worker ignores a code change | Stale container | `docker compose up -d --build worker` |

### Recovery

There is no resume, no checkpoint, and no partial-state store. Recovery is
"fix the cause and run it again", which is safe because output is regenerated
whole.

The exception is `sync-1p`. Its 1Password write is not retry-safe, so a run that
fails *after* that write may have left an item behind. Check the vault before
re-running rather than assuming a clean slate.

`setup-keycloak` is the other exception, for a different reason: a failure part
way through leaves **partially provisioned external state** — a realm, clients,
groups or users that exist while later ones do not. Re-running usually
continues cleanly, because provisioning treats 409 Conflict as success, but
"safe to re-run" is not the same as "left no trace". Inspect the realm and the
per-environment error output rather than assuming the retry started from
nothing.

For the file-writing commands a failed run leaves at worst a partially-written
directory, which the next successful run overwrites.

## Routine care

**Nothing here deletes.** Superseded generated files are removed by hand,
deliberately — `CLUSTER_APPS_DIR` can hold hand-authored files, and the GitOps
repo applies it with `prune: true`, so a wrong glob would delete live workloads.

**Hand-edits to generated files do not survive.** Change the template in this
repo instead. Applies to the ArgoCD ApplicationSet in particular, including
migration settings like `preserveResourcesOnDeletion`.

**Review the diff before pushing.** These are generated commits into
repositories ArgoCD acts on automatically. `git diff` in `DEST_DIR` and
`CLUSTER_APPS_DIR` is the last human checkpoint before the cluster converges on
whatever was produced.

**Never commit `.env`, or any extraction dump.** `.gitignore` covers the known
shapes, but the check that matters is looking at what you staged.
