---
name: independent-commit-review
description: Adversarial, fresh-eyes review of already-committed local work before it is pushed or fanned out into PRs - one independent subagent per commit with zero prior context, skeptical "stranger's PR" framing, findings fixed with revert-and-confirm verification, then history rebuilt cleanly via cherry-pick so each commit looks correct on first read. Use before pushing a batch of commits, before a batch-review fan-out, or whenever the user asks for an independent, adversarial or fresh-eyes review of local commits.
---

# Independent commit review

Don't review your own work and call it independent. The session that wrote the
code carries its author's assumptions; this buys genuinely fresh eyes by giving
each commit to a subagent that has never seen the conversation.

Ported from the `keel` template and adapted to this repo. The procedure is
general; the repo-specific parts are marked below.

**This is the strong form of [`batch-review`](../batch-review/SKILL.md)'s
Phase 2.** That phase says buy review before fan-out, because a finding costs
an amend then and a reactive round afterwards. This is how to do it properly
when the batch is large enough to be worth the subagent spend. Run it *before*
`gh stack init`, not after.

**Dispatching subagents is an operator decision here.** Do not start this
process unsolicited; it is invoked, not inferred.

## Procedure

1. **Safety snapshot.** Before any history rewrite:
   `git branch snapshot/<branch>-<date>` at the current tip. Kept permanently.
   It costs nothing and it is the only thing standing between a botched
   cherry-pick and lost work.

2. **Identify review units.** One per commit by default. Group only when
   tightly coupled (a change and its immediate fixup). Skip pure docs commits
   unless they carry risk — CI changes, `.gitignore` (a wrong entry hides a
   file from every future `git add`), or anything touching `AGENTS.md`, whose
   rules other sessions will follow without re-deriving them.

3. **Dispatch one independent subagent per unit, in parallel.** Each prompt gets:
   - the exact commit SHA, and instructions to review `git show <sha>` as a
     stranger's PR;
   - a pointer to read `AGENTS.md` first — this repo's invariants *are* the
     review criteria, and its Gotchas list is a catalogue of what has already
     bitten;
   - adversarial framing: assume the diff contains at least one real problem
     and hunt for it. Concrete things to check for *this* diff, not generic
     advice;
   - the two mandatory questions below, where they apply;
   - a capped output: severity-ranked findings, ~400–600 words, no praise.

   **Mandatory question 1 — the boundary.** For anything crossing a
   workflow/activity boundary: *what exactly crosses, and is any of it secret
   or unbounded?* Temporal records every activity result and workflow input in
   durable, readable event history. "It's only metadata" is a finding to
   examine, not a reason to move on.

   **Mandatory question 2 — determinism.** For anything reachable from workflow
   code: *does this introduce non-determinism?* Clocks, randomness, map
   iteration order, filesystem order. `internal/domain`, `internal/manifest`
   and `internal/transformers` are documented as safe to call from workflow
   code, so an impurity added there is a replay hazard, not a style problem.
   This is not hypothetical: a `crypto/rand` call reached `internal/domain` and
   shipped, one call site from a production replay failure.

4. **Triage live** as agents report: must-fix in the commit / worth a comment,
   not code / defer to the Notion tracker. **A finding is a claim, not a
   verdict** — reproduce before accepting, and expect to decline some. A round
   that accepts everything means the review is being deferred to rather than
   assessed.

5. **Fix with revert-and-confirm.** For every accepted bug: write the
   regression test, confirm it passes with the fix, revert *just the fix*,
   confirm the test fails **for the right reason**, restore the fix.

   A test never seen failing has proven nothing. This step has repeatedly
   earned its keep here: it caught a warning that could never fire (the logger
   it wrote to was never set in production), a merge that satisfied every
   override assertion while silently reshuffling field order, and — on the
   first attempt at one of these proofs — a "failure" that was a build error
   from an unused import rather than the assertion firing at all. Read *why*
   it failed, not just that it did.

6. **Rebuild history** so each commit is correct on first read — no trailing
   "fix review comments" commits. From the original base on a fresh branch,
   cherry-pick each commit in order, amending fixes into the commit they belong
   to.

7. **Verify clean, then swap.**

   ```bash
   gofmt -l ./cmd ./internal      # must print nothing
   go vet ./...
   go build ./...
   go test ./... -count=1 -shuffle=on
   ```

   `-count=1` defeats the test cache, which will otherwise report a pass for
   code that no longer exists in that arrangement. `-shuffle=on` catches
   order-dependence introduced while amending. `ruby-legacy/` is outside the Go
   module and is not covered by any of these — never "fix" anything in it.

   Then point the real branch at the rebuilt history, keeping the snapshot
   branch from step 1.

## When not to run this

- **After anything is pushed.** Step 6 rewrites history. Once a commit is on a
  reviewed PR, use `batch-review`'s followup instead — rewriting under a live
  review destroys the anchors reviewers commented against.
- **For a single small commit.** The subagent spend is justified by breadth. One
  commit gets a careful self-review and the Phase 2 pointed question.
