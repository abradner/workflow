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
gofmt -l .                     # must print nothing (matches CI; covers the top-level platform packages too)
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

**Merge style** — depends on the flavour, and the difference is the whole
reason Phase 1 is an operator choice:

- **Manual**: interstitials land as plain merge commits (`gh pr merge --merge`),
  each wrapping exactly one reviewed commit. The followup squash-merges.
- **Stacked**: the platform's "Squash and merge stack" lands one squash commit
  per PR, bottom-up, with per-PR attribution. Per-PR merge commits are not
  available — that is the trade accepted at Phase 1.

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

As of 2026-08 this repo probes **stacked available**, and GitHub stacking is
now **GA** (the operator has said to prefer this flavour where relevant). The
per-PR merge-commit trade still stands: a stacked batch lands its
interstitials squash-merged. **Name that trade in the fan-out report; the
operator has accepted it as the default here.**

Record the flavour in each PR's Batch block. **A batch normally finishes in the
flavour it started**, and stacked → manual via `gh stack unstack` is the routine
mid-batch transition.

**The reverse is permitted for one specific reason**, added after batch
`op-contract-docs` (#5–#7) took it: the manual flavour's payoff is a merge commit
wrapping *exactly one* reviewed commit, so once an interstitial has grown to
several commits that payoff is already gone and the stacked squash train is
strictly easier to merge. Switching then costs nothing and buys a cleaner train.

Adopt existing PRs with **`gh stack link <branch|pr>...`** (bottom to top), not
`gh stack init --adopt`: `link` operates GitHub-side, uses the open PRs as they
are, and needs no local tracking state — so branch SHAs are untouched and
existing review context survives. Verify afterwards that each PR's base is
unchanged and no branch was rewritten. Then update every Batch block: flavour,
the reason, and the fact that merging is now the web UI stack button.

Do not take this transition to escape a problem — that is what the one-way
degrade is for. Take it only when the multi-commit condition genuinely holds.

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

**For a batch big enough to justify it, the strong form of this phase is
[`independent-commit-review`](../independent-commit-review/SKILL.md)** - one
fresh subagent per commit, with revert-and-confirm on every fix. Run it before
`gh stack init`, never after anything is pushed: it rewrites history, and doing
that under a live review destroys the anchors reviewers commented against.

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

Triage on merit. **A finding is a claim, not a verdict** — the synthesis is
where that judgement gets made, and it is the whole reason feedback is held
until this point rather than acted on as it arrives.

- **Claims about runtime behaviour**: verify against the code. Fix only what you
  can trace or reproduce.
- **Ask whether the finding is reachable.** It can be correct about the external
  world and still irrelevant, because this codebase cannot produce the input it
  describes. Guarding an unreachable path costs maintenance and buys nothing.
  *(Batch `op-contract-docs`: a reviewer correctly noted the real CLI rejects
  unknown item categories, but the category our client sends is a constant in
  its own template map — modelling it would have meant maintaining an allowlist
  to guard a path that cannot be taken. Declined.)*
- **Check which end is wrong.** When a comment reports code and documentation
  disagreeing, the documentation is often the side to fix. *(Same batch: a
  reviewer flagged the fake's error string as not matching the notes. The fake
  was right; the notes had dropped a prefix.)*
- **Read what the finding actually supports.** A comment can be correct that
  wording is misleading without being correct that the underlying rule is wrong.
  Fix the wording, not the rule. *(Same batch: a reviewer noted the
  config-loading invariant appeared to condemn two existing child-workflow
  inputs. The invariant was right; its phrasing invited an unnecessary
  refactor.)*
- **Weigh the context the reviewer had.** A bot reviewing one PR cannot see a
  decision made three PRs earlier. Severity badges and assertive phrasing are
  formatting, not evidence.
- **Comments re-litigating deliberate design**: mark not-relevant with a
  one-line reason. **This branch must actually fire sometimes** — a synthesis
  that accepts 100% of findings is a warning sign, not a success metric. The
  sharpest recorded failure of this workflow was an all-accepted streak in which
  a reviewer citing a convention talked a deliberate safeguard out of the design.

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
- **A green CI run may not have run the jobs that matter.** Path filtering can
  skip the half of the suite your change lives in, and the rollup is green
  either way. Check which jobs actually executed, not the colour. (From keel's
  generalised version of this workflow.)
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

**Pre-flight, in order. Both checks, every time.**

1. **Does a followup exist, and is it in the train?** The whole write-only
   bargain is that feedback gets answered *somewhere*; if the stack merges
   without a followup, every accepted finding silently becomes debt on `main`
   with nothing tracking it. Nothing in the mechanics prevents this — the
   stack is perfectly mergeable without one, and the button does not ask.

   ```bash
   gh api repos/{owner}/{repo}/stacks/<n> --jq '[.pull_requests[].number]'
   ```

   If there is no followup: either there was genuinely no feedback to action
   (check — "zero unresolved threads" reads identically to "never reviewed"),
   or synthesis has not happened yet and this phase is premature. Say so and
   stop.

   *This is not hypothetical.* Batch `op-upsert` (#11–#19) merged all nine
   interstitials with no followup, leaving thirteen accepted findings on `main`
   — including a real bug (a warning that could never fire, because production
   never set the logger it wrote to) and a determinism trap in a package
   documented as pure. They were recovered only because the synthesis notes
   happened to still be in a session. A later batch would not be so lucky.

2. **Is the followup's base the cap, not `main`?**
   `gh pr view <followup> --json baseRefName`. The Phase 6 draft-first opening
   deliberately targets `main` for the aggregate review; if the retarget was
   missed, squash-merging it collapses the entire stack into one commit.

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

**There is no release phase in this repo.** CI builds and tests; nothing
publishes an artefact from a tag or a push. A merge here really is the end of
the line. (The source workflow had a Phase 8 that cut a release tag — omitted
deliberately rather than forgotten, so nobody goes looking for a `bin/release-tag`
that does not exist.)

## Stacked flavour — deltas from the phases above

Everything not listed here is unchanged: the policy phases apply as written.
Write-only feedback, the self-review, the round cap, the showstopper bar and
the Phase 7 operator gate are all identical. Only the branch and merge plumbing
differs.

**Status here.** Stacking is GA (2026-08) and this repo has run two stacked
batches: `op-contract-docs` (#5–#7 plus followup #9, merged via the web
"Squash and merge stack" button) and `platform-extraction` (#22–#26 plus
followup #28 as stack #27, merged via `gh stack merge` from the CLI).

What those batches actually confirmed, as distinct from what is still inherited:

- **`gh stack link` adopts existing PRs safely.** Used to convert a
  hand-managed stack mid-flight; branch SHAs were untouched, bases survived,
  and review context on all three PRs was preserved.
- **Linking a followup into the stack works** (`gh stack link <stack-number> <branch-or-pr>`),
  and the squash train merged it last without incident.
- **The platform merge gate is real**: `gh pr merge` refuses stacked PRs.
  But GA added `gh stack merge` (below), which an agent CAN run - so the
  operator gate is **policy again**, and binds exactly as it always did.
- **GA `gh stack link` handles fresh stacks and followup insertion**
  (batch platform-extraction): `gh stack link 22 23 24 25 26` created stack
  #27 from hand-opened PRs with branch SHAs untouched, and re-running it
  with the followup appended (`gh stack link 22 23 24 25 26 28`) inserted
  the followup as the sixth member.
- **GA auto-deletes head branches on merge** - the manual flavour's
  careful retarget-then-delete choreography is handled by the platform.

Everything else below is still inherited from a sibling repo's probe and
**unconfirmed here** — in particular `gh stack rebase`/`sync` behaviour and the
`unstack` degrade path, which no batch has yet had cause to exercise. The
extension is GA now, so withdrawal risk has receded — but keep the manual
flavour current anyway; it remains the degrade path.

- **Fan-out (Phase 3).** Branch via the tooling: `gh stack init <b1>`, commit,
  `gh stack add <b2>`, commit, … Use **virgin branch names** — `init`/`add`
  silently adopt an existing local branch of the same name, stale base and all.
  `gh stack submit --auto` opens the PRs **as drafts**: flip each ready
  immediately (`gh pr ready <n>`) so Copilot's round and CI start. The Batch
  block keeps its policy lines plus Flavour; Position, parent and roster render
  natively in the stack badge, so there is no roster sweep and no PR-number
  guessing.
- **Worktree hazard.** Every trunk operation uses the *local* `main` ref. A
  stale primary checkout makes `init` base branches on old history and makes
  `rebase` report "✓ rebased onto main" without doing it. Keep the primary
  checkout's `main` fast-forwarded and verify every claimed trunk rebase with
  `git merge-base --is-ancestor origin/main <branch>` — trust the ancestry
  check, never the ✓. **This repo is especially exposed**: the working style
  here is several worktrees off one primary checkout, and that primary sat 21
  commits behind `main` for most of one session.
- **Bake (Phase 4).** Main moved: `gh stack rebase`, then push with `gh stack
  submit` — **`gh stack sync` reports "✓ synced" without pushing rewritten
  history.** Tool-managed restacks are the recorded exception to "never rebase
  a reviewed branch": review threads were observed surviving cascade rebases
  and re-anchoring to new SHAs. Spot-check `position != null` on threads
  afterwards anyway — that observation is empirical, not contractual. A
  showstopper fix propagates by amending the interstitial's commit, `gh stack
  rebase`, `gh stack submit`, then verifying the fix **by content** on each
  child.
- **Followup (Phase 6).** Open it as an ordinary draft targeting `main`,
  *outside* the stack object, and run the aggregate `@codex review` as written.
  After harvesting: retarget to the cap, link it into the stack object with
  `gh stack link <stack-number> <branch-or-pr>` (args bottom-to-top), then mark
  ready. **Confirmed on batch `op-contract-docs`**: the link succeeded against
  a live batch and the squash train merged the followup last, in order. If it
  ever does misbehave, don't fight it — leave the followup an ordinary PR on
  the cap and finish with the manual Phase 7 choreography.
- **Merge (Phase 7).** GA gives two controls, and **both obey only the
  operator's explicit go-ahead** — `gh pr merge` still refuses stacked PRs,
  but `gh stack merge <stack-number> --yes --squash` runs the whole train
  from the CLI (atomic, all-or-nothing, bottom-up, one squash commit per
  PR; confirmed on batch platform-extraction, which also confirmed head
  branches are auto-deleted afterwards). Always pass an explicit merge
  method: without one, --yes silently reuses the last-used method. The web
  UI's "Squash and merge stack" button remains equivalent. Do **not** use
  merge-commit mode expecting per-PR merge boundaries: observed (beta), it
  lands the raw commits under a single wrapper merge of the *top* PR.
  Pre-flight (followup in the train, followup based on the cap) is
  unchanged and matters MORE now that the train is one command.
- **Fallback.** If anything breaks mid-batch — extension vanished, stack API
  errors, an operation misbehaves (a conflict prompt is not misbehaviour) —
  degrade once, permanently, for that batch: `gh stack unstack` dissolves the
  stack object, observed non-destructive (PRs stay open with correct bases,
  branches intact). Then finish under the manual phases. `gh pr merge` works
  again once the stack object is gone, which also means the platform's merge
  block is gone: **the operator gate reverts to policy**, and binds exactly as
  it always did.

## Rules of thumb

- **Probe the flavour first; record it in every Batch block; finish the batch
  in the flavour it started.** The one exception is the mid-batch degrade,
  stacked → manual via unstack — never the reverse.
- **Self-review before fan-out.** One pointed question per branch, naming what
  the change made newly risky. A finding caught there costs an amend; the same
  finding after fan-out costs a round, and rounds introduce bugs.
- Feedback is write-only until synthesis. No replies, no mid-stack pushes.
- Interstitials get Copilot's one round on open. Codex is invocation-only and
  aggregate-first: 2–3 invocations per batch, total.
- Interstitials: one commit each, merge-committed. Followup: N commits,
  squash-merged, release gate.
- Merge `main` into a batch branch only when the world moved for reasons
  outside the batch — then effective-diff audit afterwards.
- Merge bottom-up. Never rebase a reviewed branch (manual flavour). Retarget
  each child before deleting its parent's branch.
- Phase 7 needs an explicit, per-batch go-ahead from the human operator — never
  self-initiate the merge train, including under autonomous or auto-mode
  operation.
- **Before the train: confirm a followup exists and is in it.** A stack merges
  perfectly well without one, and then the accepted findings are just debt on
  main. Observed on batch op-upsert.
- Only the followup's CI gates the batch. Unexplained interstitial red is a
  real signal.
- The interstitial showstopper bar is irreversible loss on merge, nothing else.
- Defer everything that isn't a showstopper and **write the deferral down as a
  Notion task**, not a table in the PR body — the body goes stale between
  rounds and vanishes on merge.
- **Cap reactive rounds at three.** Past that, fix only genuine defects in
  shipped behaviour and ticket the rest. If one small change draws three or
  more findings, revert and ticket it rather than patching again.
- **Never trust an aggregate review signal.** Verify by `commit_id`, open
  Copilot's suppressed `<details>` block, read its "reviewed N out of M files"
  count, and treat a new test that passes on its first run as unproven until
  shown failing.

## Where these rules came from

Inherited from a batch in the source repo that ran seven reactive rounds with
findings going **6 → 2 → 1 → 3 → 2 → 5** — not convergence. The fan-out worked:
26 real findings across six interstitials, several of them data-loss bugs. The
reactive tail did not: four of five later rounds found defects created by a
*previous round's fix*, three tests written to prove fixes passed against
unfixed code, and one eight-line change drew three defects before being
reverted and ticketed.

The through-line: **review is worth most before the work is published and
progressively less after**, because every later finding is fixed under pressure
against a stack that resists change. Hence the self-review phase, the round cap,
and ticket-don't-patch.

The economic half: the AI spends are all drained by the same event — a reactive
round — so the round cap is the cost lever on all of them at once. That is also
what split the bot roles: Copilot's per-PR review is cheap and empirically
strong at mechanical bug classes, so it runs everywhere; Codex is quota-bound
and strongest cross-file, so it is spent on the aggregate.

This repo's own contribution so far is the Phase 2 example: a fix written
against a misdiagnosis, where the verification that would have prevented it
took one command. Add to this section rather than replacing it — the value is
in the accumulated evidence, not in any single batch.
