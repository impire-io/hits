# Plan 014 — the tracker auditor

## Layout

```
internal/cli/audit.go       runAudit: flag parsing, the dense GetItem
                            walk, ref checks, merge scan, findings,
                            output; git evidence via exec'd git
internal/cli/audit_test.go  harness + real temp git repos: every
                            finding kind, the clean pass, exit and
                            JSON behavior, pre-dial rejections
internal/cli/cli.go         usage text + dispatch case
```

## Mechanics

- **One git read per clone, checks in-process.** Each mapped clone is
  loaded once — resolve main (`origin/main`, else `main`), read
  `git log --format=%s` subjects, parse `origin`'s URL to `owner/repo`
  — into a `repoEvidence` value. PR-subject matching and the merge scan
  run over those subjects in Go (exact-number regexps), never through
  `--grep`. Only the commit-ancestry check execs per ref
  (`merge-base --is-ancestor`: exit 0 ancestor, 1 not, anything else —
  unknown sha included — not on main, with git's words in the detail).
  A commit ref resolves its repo like a pr ref when qualified
  (`owner/repo@sha`, the form the live corpus carries) and through
  located-in when bare.
- **The walk is the enumeration.** `GetItem("1"), GetItem("2"), …`
  until the APIError code `not-found` — dense server-minted IDs make
  the first gap the end of the corpus. The gets run a window of
  `--fan` at a time (default 8, the search table's resolver bound) so
  the wire round-trips overlap rather than queue; density means
  everything past
  the first gap is discarded, errors included. Clone evidence loads
  fan out the same way, one goroutine per mapping. The per-ref checks
  stay sequential and in item order — in-memory scans plus the odd
  local git exec, where deterministic output is worth more than the
  microseconds. Tombstoned snapshots are returned by the service
  (tombstone is a fold flag, not a delete) and skipped here.
- **Findings are one flat list.** `{level, kind, item, repo, ref,
  detail}` with kinds `pr-unmerged`, `commit-unmerged`, `merged-open`,
  `merged-untracked` (failures) and `unverifiable` (warning) — the
  machine-legible-errors posture, stable vocabulary for CI to grep.
  Failures make `runAudit` return an error after printing, so the
  process exits non-zero exactly when the record is contradicted.

## Tests

The embedded-NATS harness seeds the tracker through the CLI itself;
`gitRepo` builds throwaway repos with exec'd git (isolated config:
`GIT_CONFIG_GLOBAL=/dev/null`, signing off, `init -b main`), commits,
no-ff merges with GitHub-form subjects, and a fake `origin` URL for the
identity. Covered: the all-green audit; each failure kind; the
unverifiable warning (unmapped identity, unqualified pr ref, unmapped
located-in) exiting zero; action-only refs producing nothing;
tombstoned items skipped; squash-merge subjects matching check A but
invisible to check B; `--json` shape; no `--repo` and a bad mapping
rejected before dialing (guard connector).
