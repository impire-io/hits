# Spec 015 — initiatives

**Work ID:** `003-group-isolation`
**Design:** `hits-hq` @ `e868cdf` —
[`03-DECISIONS/0016-initiatives.md`](../../../hits-hq/03-DECISIONS/0016-initiatives.md)
(the concept, the ID scheme, the settled review answers),
[`02-DESIGN/item-model.md`](../../../hits-hq/02-DESIGN/item-model.md)
§ identity, § initiatives, § invariants,
[`02-DESIGN/ops-log.md`](../../../hits-hq/02-DESIGN/ops-log.md)
§ subjects, § op catalog, § identifiers, § the state projection,
[`02-DESIGN/services.md`](../../../hits-hq/02-DESIGN/services.md) and
[`02-DESIGN/mcp-server.md`](../../../hits-hq/02-DESIGN/mcp-server.md)
(the surface); rooted in research `003-group-isolation`.
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting
review and merge.

## What this delivers

One HITS install tracks many groups of projects. An **initiative** is
the second registered vocabulary — `{slug, name, description}` on
`hits.ops.initiative.<slug>`, with the 0015 lifecycle: register and
retire, reason required, retired slugs never reused. Every project
names its initiative; every new item ID is initiative-prefixed
(`hits-19`) from that initiative's dense counter, making the item's
initiative intrinsic from filing. An initiative is a lens, not a wall:
nothing restricts reads; search and the audit gain explicit
`--initiative` scope, the graph materializes initiative nodes, and the
client config gains a *selected* initiative that feeds filing only.

## Out of scope

- **No renumbering.** Legacy bare IDs stand forever; the bare counter
  freezes. A legacy item takes its initiative by ordinary edit — the
  only place item initiative is writable.
- **No access control, no per-initiative streams or prefixes** —
  decision 0004 untouched; restrictions, if ever, live at the account
  edge (auth-callout), outside this build.
- **No envelope change.** The subject kind plus the envelope stays the
  dispatch rule; initiative ops reuse `registered`/`retired`, told
  apart by subject.
- **The live backfill.** Registering `hits` and `chronicle`, assigning
  projects and legacy items on the install is a validation-time
  operation after release, not code in this build.

## Requirements

- **FR-01 Contract.** `Initiative` joins the model; `Project` and
  `Item` carry `initiative`. The op catalog grows `assigned` (project:
  its initiative — a move or the backfill) and initiative
  `registered`/`retired`; `created` echoes the item's initiative,
  `edited` may carry it. `ParseItemID` splits any ID (bare or
  prefixed) by the trailing-digits rule; initiative slugs are refused
  a trailing all-digit segment (`ValidInitiativeSlug`).
- **FR-02 Invariants.** A create names a registered, unretired
  initiative (validated before minting, so a rejected create burns no
  number). `located-in` projects must belong to the item's initiative
  when both sides carry one — pre-backfill blanks pass. `initiative`
  on edit is legal only on bare-ID items (`initiative-immutable`
  otherwise). Project registration requires an initiative; assignment
  requires a live one. All existing invariants stand.
- **FR-03 Node.** Per-initiative counters
  (`system.item-counter.<slug>`) mint `<slug>-<n>`; the bare counter
  is never advanced by a mint again. Replay derives every counter from
  the log per initiative (legacy included) and routes folds by subject
  kind — item, project, or initiative. New endpoints:
  `initiative.register` / `initiative.retire` / `initiative.list` and
  `project.assign`, mirroring the project endpoints' CAS discipline.
- **FR-04 Client.** Request types and methods for the new endpoints;
  `CreateItemRequest.Initiative`, `EditItemRequest.Initiative`,
  `SearchRequest.Initiative`.
- **FR-05 Search.** The index maps `initiative` as a keyword field
  from the snapshot and the query filters on it, beside type and
  status.
- **FR-06 Graph.** Initiative nodes materialize: names from their
  `registered` ops, `in-initiative` edges derived project → initiative
  (from registration and assignment, folded project state) and item →
  initiative (from the snapshot). Blocked-by edge derivation accepts
  prefixed item refs.
- **FR-07 CLI.** `hits initiative register|retire|list|select` —
  `select` verifies the slug against the live registry, then records
  it in the client config beside the default actor; it feeds filing
  only. `create` resolves its initiative as flag → `$HITS_INITIATIVE`
  → selected default, and fails without one, before dialing where
  possible. `edit --initiative` (legacy items), `project register
  --initiative` (required), `project assign`, `search --initiative`.
- **FR-08 Audit.** The walk enumerates the legacy range and every
  registered initiative's range (dense per sequence); `--initiative`
  narrows the tracker side of the audit. The merged-work scan reads
  prefixed work IDs and keeps resolving historical bare-integer
  branches.
- **FR-09 MCP.** Tools `register_initiative`, `retire_initiative`,
  `list_initiatives`, `assign_project`; `create_item`/`edit_item`/
  `search_items` inputs grow their initiative fields. The server takes
  an optional startup default initiative (flag → `$HITS_INITIATIVE` →
  config default), feeding `create_item` when the call names none —
  one process, one filing default, never a read scope.
- **FR-10 Tests.** Everything against real NATS via the embedded
  harness (race on) and, for the audit, real temp git repos; the
  contract fold, checks, and ID parsing tested pure.
