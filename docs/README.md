# Documentation

Four kinds of document live here, and they answer different questions. Start
with the one matching what you actually need.

| If you want to… | Read |
|---|---|
| Understand what this tool *is* and how the pieces relate | [ARCHITECTURE.md](ARCHITECTURE.md) |
| Run it, or work out why a run went wrong | [OPERATIONS.md](OPERATIONS.md) |
| Change the code | [modules/](modules/README.md) |
| Learn the Go and Temporal concepts this codebase leans on | [GO_NOTES.md](GO_NOTES.md) |

Two further documents are narrower in scope:

- [MIGRATION_PLAYBOOK.md](MIGRATION_PLAYBOOK.md) — how the Ruby original was
  ported, written to be reusable against a second, similar codebase. Historical
  once that job is done.
- [OP_CLI_NOTES.md](OP_CLI_NOTES.md) — observed behaviour of the 1Password CLI,
  and the evidence behind the test fake that models it. Read before touching
  anything that shells out to `op`.

At the repository root, [README.md](../README.md) is the short front door
(what the five commands do, how to run one) and [AI_ONBOARDING.md](../AI_ONBOARDING.md)
orients coding agents, including the Ruby→Go file map.

## The three levels

Documentation here is deliberately layered, because the questions are different
at each level and mixing them produces a document that serves nobody.

**Abstract** — `ARCHITECTURE.md`. What the system does, what it talks to, and
the handful of design rules that explain why the code is shaped the way it is.
No function signatures. Should stay true across a refactor.

**Technical** — `modules/`. Package by package: responsibilities, boundaries,
the invariants each one holds, and the traps. Names real types and functions,
so it dates faster and is expected to be updated alongside the code.

**Operational** — `OPERATIONS.md`. Prerequisites, running each workflow, what
failure looks like, and what to do about it. Written for someone at a terminal
with something broken.

## Keeping these honest

Docs that quietly drift are worse than absent ones, because they are trusted.
Two specific commitments:

- `OP_CLI_NOTES.md` records a CLI version and a date. When `op` is upgraded,
  re-run the transcript in it and correct both the notes and the fake they
  justify.
- `modules/` names real identifiers. When you rename or move one, the grep that
  finds the code will find the doc too. Fix it in the same change.
