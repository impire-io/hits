# Spec 014 — the tracker auditor

**Work ID:** `17`
**Design:** `hits-hq` @ `56c6b21` —
[`03-DECISIONS/0013-issue-tracking-cutover.md`](../../../hits-hq/03-DECISIONS/0013-issue-tracking-cutover.md)
§ consequences (the auditor's mandate),
[`00-META/process/03-issues.md`](../../../hits-hq/00-META/process/03-issues.md)
§ recording the refs (what a verifiable ref is),
[`00-META/how-we-build.md`](../../../hits-hq/00-META/how-we-build.md)
§ work in isolation (one work ID names everything — the branch is the
bare item ID); rooted in hits item `17`.
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting
review and merge.

## What this delivers

`hits audit` — the tracker-side replacement for the file-era
`check-claims.sh`/`check-unclaimed.sh` CI guards that decision 0013
declared impossible against frozen frontmatter. Given a mapping of
project slugs to local clones, it reads every item from the tracker and
holds the record against git:

- **Ref honesty.** A resolved item's `fixed-by` refs must be true on the
  named repo's `main`: a `pr:owner/repo#N` ref needs a commit on main
  whose subject references the pull request (GitHub merge or squash
  form), and a `commit:<sha>` ref must be an ancestor of main in a repo
  the item is located in.
- **Close after merge.** Merged work must not leave its item open: a
  merge commit on main from a bare-integer branch names a work ID
  (playbook 07's naming rule — post-cutover branches are item IDs,
  pre-cutover branches were named slugs), and that item must exist and
  be terminal.

Contradictions are failures and fail the command — the CI-guard posture.
Refs the given mapping cannot check are warnings: reported, never
silently dropped, but not a failure of the record itself.

## Out of scope

- **No wire or contract change.** Like spec 012's search table, this is
  a client-side composition: tracker state through the `client` package,
  git truth through the local clones. No new endpoint, hence no new MCP
  tool (the 1:1 rule binds tools to endpoints).
- **No GitHub API.** Git history is the evidence; the auditor runs where
  the clones are. Fetching is the operator's job — the audit reads
  `origin/main` where it exists (falling back to a local `main`) and
  never touches the network.
- **Squash-merge blindness in the close-after-merge check.** A squash
  merge does not name its branch, so it cannot yield a work ID — the
  same boundary playbook 03 already states for `pr:` verifiability.
- **`action:` refs.** Not git-verifiable by design; the note carries the
  evidence (playbook 03). Skipped without comment.
- **Slug→repo resolution from the tracker.** Projects carry no repo URL
  (`repos.md` is the hand-mirror), so the mapping is the caller's
  argument, not discovered state.

## Requirements

- **FR-01** `hits audit --repo <slug>=<path>` (repeatable) audits the
  tracker against the named clones. At least one mapping is required,
  rejected before dialing. Each clone's GitHub identity (`owner/repo`)
  derives from its `origin` remote URL (ssh, https, or ssh:// forms);
  a clone without one simply has no PR identity.
- **FR-02** The audit reads every item by walking `GetItem` from ID 1
  until the first `not-found` — IDs are server-minted dense integers,
  so the walk is complete by construction and no index is consulted
  (the index is never authority). Tombstoned items are skipped.
- **FR-03** For each resolved item, every `pr:owner/repo#N` ref is
  verified against the mapped clone with that identity: some commit
  subject on main matches `Merge pull request #N` or `(#N)` (exact
  number). No match is the failure `pr-unmerged`. A ref without an
  `owner/repo#N` form, or naming no mapped clone, is the warning
  `unverifiable`.
- **FR-04** For each resolved item, every commit ref is verified as an
  ancestor of main (`git merge-base --is-ancestor`). The ref names its
  repo in one of two forms the corpus carries: `commit:owner/repo@sha`
  checks the mapped clone with that identity (as pr refs do), and a
  bare `commit:<sha>` checks every mapped clone of the item's
  `located-in` slugs. Not an ancestor anywhere checked (unknown shas
  included) is the failure `commit-unmerged`; no mapped clone to check
  is the warning `unverifiable`.
- **FR-05** For each mapped clone, every merge subject on main matching
  `Merge pull request #N from <owner>/<digits>` names a work ID. An
  item that is missing or tombstoned is the failure `merged-untracked`;
  an item that is not terminal is the failure `merged-open`.
- **FR-06** Main resolves per clone as `origin/main`, falling back to
  `main`; a clone with neither, or an unreadable mapping, fails the
  command with a clear error naming the mapping.
- **FR-07** Findings print one per line with level, item or repo, the
  ref or merge subject, and the reason; a summary line counts items and
  repos audited, failures, and warnings. `--json` emits the findings
  and counts as one document. Any failure makes the command's exit
  non-zero; warnings alone do not.
- **FR-08** Tested against real NATS via the embedded harness (race
  detector on) with real temporary git repositories — no mocked git,
  no mocked NATS.
