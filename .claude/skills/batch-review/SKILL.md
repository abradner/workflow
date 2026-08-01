---
name: batch-review
description: Ship a body of work as a stack of small atomic single-commit PRs (interstitials, base to cap), let CI and Copilot review immediately but treat all feedback as write-only until the whole batch is in, then synthesise the feedback in aggregate, ship all reactive work as ONE followup PR stacked on the cap, resolve interstitial comments as "fixed in the followup" or "not relevant", and merge bottom-up on the operator's explicit say-so. Use whenever the user wants to review open PRs holistically or in aggregate, process accumulated bot review comments across several PRs, ship a "review feedback batch" or followup PR, or refers to a batch of work fired off earlier - even without saying "batch review". ALSO use at fan-out time, when the user asks to build a planned body of work as small/stacked PRs or hands off a stack before signing off; the fan-out discipline (never react to feedback mid-stack) is what makes the rest cheap.
---

# Batch Review

Ship a large body of work as many small PRs without drowning in drip-fed
review cycles. Each PR is the mechanism for getting per-unit CI and review on
one slice. The core bargain: **feedback is write-only until the whole batch is
synthesised.** CI and the bots run immediately on every PR and comments
accumulate freely — but nothing responds to them and no reviewed branch is
rewritten. All reactive work lands once, in a single followup PR, and the
stack merges bottom-up.

Ported from the `spritz` repo's workflow of the same name and adapted to this
repo. The discipline is general; the specifics below are not.

## Terminology

- **Interstitial** — any PR in the proactive stack, bottom to top. Exactly
  **one commit** each, so the merge commit that lands it wraps exactly the
  reviewed change.
- **Base** — the bottom interstitial, the one targeting `main`.
- **Cap** — the top interstitial. The end of the proactive work.
- **Followup** — the reactive PR, created only after synthesis, stacked on the
  cap. All reactive work happens here. May be N commits; it is
  **squash-merged**, so its history is free to be messy.

Why it works:

- Small single-commit interstitials keep review bots on detail instead of lost
  in a diverse changeset, and let the operator hold the batch in their head.
- Opening everything ready-for-review starts CI and the feedback window at once.
- Not reacting mid-stack means `main` never moves under the stack, comments stay
  anchored to real SHAs, and no PR is superseded by its own feedback loop.
- Fixing only in the followup, then merging bottom-up, avoids restacking and
  rebase drama entirely.

## Repo specifics

**Validation** — the full gate, run before opening anything and once on the
followup:

```bash
gofmt -l ./cmd ./internal      # must print nothing
go vet ./...
go build ./...
go test ./...
```

`ruby-legacy/` is outside the Go module: it is a frozen snapshot, and
`gofmt`/`go build`/`go test` all ignore it. Never "fix" anything in it.

**Review bots**

- **Copilot** (`copilot-pull-request-reviewer`) auto-reviews non-draft PRs on
  open. Cheap and ample — use on every PR without rationing. Always open any
  `<details>` "suppressed due to low confidence" block; real bugs have hidden
  under a "no new comments" summary.
- **Codex** (`chatgpt-codex-connector`) has auto-reviewed in this repo
  historically (PRs #1–#2) but did **not** auto-review #4–#7 — treat it as
  invocation-only and post an explicit `@codex review`. It is the scarce
  budget: **2–3 invocations per batch**, aggregate-first, reserved for
  cross-file correctness work. Skip it for pure docs and config unless
  accuracy-against-code is the actual question — in which case say so in the
  invocation.
- **Always ask for comments only, no commits.** An agent pushing to a batch
  branch silently breaks the write-only discipline.

**Conventions the bots should be held to** (verify the citation, then act) —
all from `AGENTS.md` and `docs/ARCHITECTURE.md`:

- **Nothing secret or unbounded crosses a workflow/activity boundary.**
  Temporal records activity results and workflow inputs in durable, readable
  event history. Bundle read-transform-write into one activity, return a count.
- **Purity discipline**: `internal/transformers`, `internal/manifest` and
  `internal/domain` stay free of I/O and Temporal imports.
- **Config is loaded by an activity**, never passed in as workflow input.
- **Generated output is regenerated whole**, and this tool never deletes files
  it did not just write.
- **The Temporal dependency triple** (`sdk`, `server`, `api`) is pinned
  mutually compatible. Never `go get -u` them independently.

**Tracker** — Notion, Tasks data source `c0e96552-0a49-4311-8d51-8f5ad7ae86a8`,
related to the project page via the `Project` relation. Set `Impact` and
`Effort level`. This is where deferred findings go instead of another round.

**Merge style** — interstitials land as plain merge commits (`gh pr merge
--merge`); the followup squash-merges.

## Phase 1 — Flavour probe

```bash
if ! gh stack --help >/dev/null 2>&1; then
  echo "manual (extension missing)"
elif out=$(gh api repos/{owner}/{repo}/stacks 2>&1); then
  echo "stacked available"
elif grep -q "HTTP 404" <<<"$out"; then
  echo "manual (repo not enrolled)"
else
  echo "transient probe failure — retry once: $out"
fi
```

As of 2026-08 this repo probes **stacked available**. That does not make
stacked the default: the beta's observed merge modes cannot produce per-PR
merge commits, so a stacked batch lands its interstitials squash-merged.
**Put that trade to the operator before fan-out; never assume it.**

Record the flavour in each PR's Batch block. **A batch finishes in the flavour
it started.**

## Phase 2 — Self-review, before any PR opens

**Every finding that arrives after fan-out has to be fixed under reactive
pressure, and that is where bugs get introduced.** The same finding surfaced
before the PRs open costs a `git commit --amend` — no CI cycle, no bot wait, no
stack churn — and gets fixed with the same care as the original work.

So buy review early, not more of it:

1. For each branch, get `git diff origin/main...HEAD` and ask **one pointed
   question** naming what the change made newly risky. Generic "review this"
   gets generic results. The useful shape is usually: *what did this change
   make asynchronous, deferred, or dependent on identity or position that was
   not before?*
2. Fix findings **in the commit itself**. Nothing is pushed, so there is no
   followup, no restack, no thread to answer.

Do not skip this because the change looks small.

**A worked example from this repo.** A `sync-1p` investigation concluded
`op item create` was broken and a fix was written adding `--category`. The
diagnosis came from hand-testing with a shell redirect (`< file.json`), which
`op` ignores silently — while Go's `exec.Cmd` gives the child a *pipe*, which
works. The code had never been broken, and the "fix" would have broken it, since
`op` rejects a category given in both the template and the flag. One
verification step before opening the PR caught it. The general rule:
**when reproducing a shell-out by hand, reproduce how the program invokes it,
not how your shell does.**

## Phase 3 — Fan-out

- Split into small, atomic, **single-commit** interstitials. Branch each off its
  parent and PR against the parent's branch; the base targets `main`.
- **Open every PR ready-for-review immediately** — CI and Copilot start at once.
  Do not spend Codex on interstitials; its aggregate pass covers the cross-PR
  seams the interstitials cannot show it.
- Each PR body states summary, root cause for fixes, validation actually run,
  and ends with the **Batch block**. The description travels into every
  reviewer's and every fresh session's context, so this is the layer that works
  even when the skill doesn't trigger:

  ```markdown
  ## Batch
  - **Batch**: <slug> (#<first>–#<last>)
  - **Flavour**: manual | stacked
  - **Position**: base (1/m) | interstitial (n/m) | cap (m/m)
  - **Stacked on**: #<parent> (base: main)
  - **Feedback policy**: write-only until synthesis — comments here are
    harvested and answered in one followup PR, not patched in place.
    Reviewers: review fully as normal; unanswered comments are the workflow,
    not neglect. Agents: do not push fixes to this branch.
  - **CI policy**: interstitial red is acceptable when explained by the stack;
    the followup is the release gate.
  - **Merge policy**: operator-initiated only.
  ```

  State only what is known at write time — **never guess ordinals, totals or
  future PR numbers, and never leave placeholders in a live description.** Stamp
  the roster in one `gh pr edit` pass when the cap opens.

- From here the discipline is **no reaction**: don't reply to comments, don't
  push fixes to reviewed branches, don't merge `main` into the stack because of
  batch feedback.

## Phase 4 — Bake

Normally nothing to do. Two contingencies:

- **`main` moved for unrelated reasons**: merge `main` into affected branches —
  **never rebase a branch that has review context**. Afterwards audit
  `git diff origin/main...HEAD --stat`; the effective diff is the truth of what
  the PR still changes, and it catches a PR silently superseded.
- **A genuine showstopper.** The bar for fixing in place is exactly one thing:
  **irreversible loss on merge** — something no followup can undo. Everything
  else waits. Nothing deploys from an interstitial here.

**Interstitial CI red is acceptable** when the failure is explained by
something above it in the stack. An *unexplained* red is real signal.

Any mid-batch event — comment webhook, CI status, monitor line — is a **ledger
entry, not a work order**. Triage against the showstopper bar, record, stand
down.

**Any human comment is an event, unconditionally.** A monitor armed only for
"a bot review appeared" is structurally deaf to the operator commenting
mid-bake. Note also that agent thread replies go out under the operator's `gh`
credentials, so key any "a review appeared" condition on the *bot* logins.

## Phase 5 — Synthesis

Harvest **all** comments across the stack:

```bash
gh pr view N --json comments                        # issue comments
gh api repos/{owner}/{repo}/pulls/N/reviews         # review bodies
gh api repos/{owner}/{repo}/pulls/N/comments        # inline — the substance
```

**Separate genuine review comments from agent replies by `in_reply_to_id`,
never by author** — agent replies post under the operator's credentials and
otherwise read as human reviews at the current SHA.

Add your own aggregate review: cross-PR interactions, sibling inconsistency
(the same problem solved two ways), and gaps *adjacent* to a PR's purpose.

Triage on merit, remembering bots only had per-PR context:

- **Claims about runtime behaviour**: verify against the code. Fix only what you
  can trace or reproduce.
- **Comments re-litigating deliberate design**: mark not-relevant with a
  one-line reason. **This branch must actually fire sometimes** — a synthesis
  that accepts 100% of findings is a warning sign, not a success metric.

## Phase 6 — The followup PR

One followup stacked on the cap, N commits, squash-merged. It carries every
actioned item grouped by source PR, a section for findings **assessed and not
actioned** with reasons, and one full validation pass.

Then resolve every interstitial thread: "fixed in the followup (#N)" or "not
relevant: <reason>". This is the only time the batch touches its own threads.

### The aggregate Codex review, draft-first

Codex's best findings are cross-file — the seams *between* interstitials, which
no per-PR diff shows. The followup is the one moment the whole batch exists as
one diff:

1. **Open the followup as a draft, base = `main`.** The diff is then the entire
   stack plus reactive work. Draft status is the safety wrapper: a followup
   targeting `main`, if merged, **squash-merges the whole stack** — every
   reviewed PR collapsed into one commit. A draft cannot be merged.
2. `@codex review` on the draft.
3. **Harvest its findings before touching the base** — retargeting narrows the
   diff and can orphan inline threads.
4. Retarget base to the cap:
   `gh api -X PATCH repos/{owner}/{repo}/pulls/N -f base=<cap-branch>`
5. Mark ready; Copilot reviews the now-narrow diff.

### Re-soliciting review on later rounds

Interstitials get one Copilot round on open by design. The followup is
iterated, and review must be re-solicited per round:

```bash
gh api repos/{owner}/{repo}/pulls/N/requested_reviewers -X POST \
  -f 'reviewers[]=copilot-pull-request-reviewer[bot]'
```

Give either re-solicitation ~10 minutes. Silence past that is a no-show:
record it and proceed.

Carry a `## Review focus` section in the followup body, current per round —
highest risk, what to verify rather than assume, and what is out of scope
because it was already decided or ticketed.

### Do not trust aggregate review signals

- **"Zero unresolved threads" reads identically whether feedback was answered or
  never solicited.** Verify by `commit_id`: a review against an earlier SHA has
  not seen the current work.
- **Copilot's "no new comments" can sit directly above real bugs** in a
  low-confidence `<details>` block. Always open it.
- **"Reviewed N out of M changed files" is a partial pass reading as a complete
  one.** Read the count; close the gap yourself in synthesis.
- **A new test that passes on its first run proves nothing.** Show every test
  written to demonstrate a fix failing first. If it won't fail, either the
  harness is wrong or the bug is not where you think — both worth knowing.

### Cap the reactive rounds

Reactive fixes have a high enough bug-introduction rate that more rounds is not
monotonically better. **Cap at three.** The cap can arrive early: **when a
round's findings are all in code the previous round wrote, that is the cap
arriving.**

At the cap, fix only genuine defects in shipped behaviour. **Everything else
becomes a Notion task, not another round** — a triage table in a PR body goes
stale and vanishes when the PR merges; a task does not.

If one small change attracts three or more findings, **revert it and ticket the
work**. Three problems from eight lines means it needs design time, not another
patch.

## Phase 7 — Merge

**Gate: never start this phase without the operator explicitly saying to merge
now.** A green followup, a synthesis pass, or an auto-mode session default is
not that signal. Pushing merges and deleting branches is exactly the
hard-to-reverse shared-state action that needs a live go-ahead each time, not
standing authorization from having approved the workflow once. If the stack is
ready, say so and stop.

**Pre-flight**: confirm the followup's base is the cap, not `main` —
`gh pr view <followup> --json baseRefName`. If the Phase 6 retarget was missed,
squash-merging it would collapse the whole stack into one commit.

Then, per interstitial bottom-up:

1. `gh pr merge <n> --merge` — **no `--delete-branch`**.
2. Poll `gh pr view <n> --json state,mergedAt` until `MERGED`. A returned
   command is not a merge.
3. Confirm the next PR's base retargeted to `main`; retarget explicitly if not.
4. Only then delete the just-merged branch.

Deleting a base branch out from under a not-yet-retargeted child **closes the
child**. If that happens: push the branch back from its last SHA, reopen, then
retarget — no work is lost, but confirm state before continuing.

The followup merges last, squash-and-merge.
