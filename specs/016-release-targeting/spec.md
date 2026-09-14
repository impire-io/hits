# Spec 016 — release targeting

**Work ID:** `hits-3`
**Design:** `hits-hq` @ `c4acaf1` —
[`03-DECISIONS/0017-release-targeting.md`](../../../hits-hq/03-DECISIONS/0017-release-targeting.md)
(the concept, the settled review answers, the refusals),
[`02-DESIGN/item-model.md`](../../../hits-hq/02-DESIGN/item-model.md)
§ properties, § invariants, § links, § releases,
[`02-DESIGN/ops-log.md`](../../../hits-hq/02-DESIGN/ops-log.md)
§ subjects, § op catalog, § identifiers, § the state projection,
[`02-DESIGN/services.md`](../../../hits-hq/02-DESIGN/services.md) and
[`02-DESIGN/mcp-server.md`](../../../hits-hq/02-DESIGN/mcp-server.md)
(the surface); rooted in research `004-release-targeting`, tracked as
`hits-3`.
**Status:** in progress on this branch ([plan.md](plan.md)).

## What this delivers

Items can aim at a release. A **release** is the third registered
vocabulary — `{slug, name, description}` per initiative on
`hits.ops.release.<initiative>.<slug>`, lifecycle
register → shipped | retired. `shipped` is terminal and carries
verifiable refs (tag, commit, artifact) plus an optional note, the way
a resolving transition carries `fixed-by`; `retired` is the 0015 exit,
reason required; neither slug is ever reused. Items get one nullable
`target` property storing the bare release slug, validated against the
item's own initiative's releases; the release view — "what must still
land in `0.5`" — is a query, and shipping is refused while any
non-terminal item targets the release, so the cut is the triage pass.
Search filters on target, the graph materializes release nodes with
derived item → release `targets` edges, and the audit nets the target
invariants in its existing walk.

## Out of scope

- **The decided refusals.** No dates on releases, no timebox axis, no
  tracked progress, no release hierarchy, no multi-release items, no
  someday bucket — absence of target is someday (decision 0017; each
  refusal maps to a documented failure mode in the survey).
- **No dedicated audit pass or release status view.** The invariants
  ride the existing whole-corpus walk; the cut-time readout is a
  scoped query, not a new surface.
- **No envelope change.** The subject kind plus the envelope stays the
  dispatch rule; release ops reuse `registered`/`retired` and add
  `shipped`, told apart by subject.
- **No reverse index for the straggler gate.** `ship` validates by
  reading open-item snapshots — shipping is rare, a scan is
  acceptable, nothing enters the write model.
- **Live registration of actual releases** on the running install is a
  validation-time operation after the release carrying the verbs, not
  code in this build.

## Requirements

- **FR-01 Contract.** `Release` joins the model; `Item` carries
  `target`. The op catalog grows release `registered` / `shipped` /
  `retired`; `created` and `edited` may carry `target`.
  `ValidReleaseSlug`: lowercase `[a-z0-9.-]`, every dot-separated
  segment non-empty and a valid subject token. The release subject
  carries `<initiative>.<slug>`; the parse is initiative = first
  token (initiative slugs are dot-free), slug = the rest rejoined,
  dots literal.
- **FR-02 Invariants.** `target` may name only a registered,
  unshipped, unretired release of the item's own initiative
  (`unknown-release`, `release-shipped`, `release-retired`). `ship`
  is refused while any non-terminal item targets the release
  (`open-targets`). Shipped and retired releases accept no further
  ops. The terminal-item edit refusal already freezes `target` at
  close; tombstoned items are dropped from projections and so never
  hold a ship hostage. Retirement stays guardless (0015): existing
  targets stand, new writes refuse the retired slug. All existing
  invariants stand.
- **FR-03 Node.** Endpoints `release.register` / `release.ship` /
  `release.retire` / `release.list`, mirroring the vocabulary
  endpoints' CAS discipline (registration at expected subject
  sequence zero; per-initiative uniqueness and the CAS scope are the
  same subject). The fold routes the release subject kind; the state
  bucket gains `release.<initiative>.<slug>` keys. `ship` validates
  the straggler gate by scanning item snapshots for non-terminal
  items of the release's initiative whose `target` names it.
- **FR-04 Client.** Request types and methods for the four endpoints;
  `CreateItemRequest.Target`, `EditItemRequest.Target` (settable and
  clearable), `SearchRequest.Target`.
- **FR-05 Search.** The index maps `target` as a keyword field from
  the snapshot and the query filters on it, beside type, status, and
  initiative.
- **FR-06 Graph.** Release nodes materialize, names from their
  `registered` ops; `targets` edges derive item → release while a
  target is set and drop when it clears or the item tombstones.
- **FR-07 CLI.** `hits release register|ship|retire|list` (register
  and list take the initiative like project verbs do; ship carries
  refs and an optional note; retire a reason). `create --target`,
  `edit --target` (with an explicit clear), `search --target`.
- **FR-08 Audit.** The existing walk nets the target invariants:
  every `target` names a known release of the item's initiative, and
  no non-terminal item targets a terminal release. No new pass, no
  new surface beyond the findings.
- **FR-09 MCP.** Tools `register_release`, `ship_release`,
  `retire_release`, `list_releases`; `create_item` / `edit_item` /
  `search_items` inputs grow their target fields. Read-only tools
  carry the read-only annotation.
- **FR-10 Tests.** Everything against real NATS via the embedded
  harness (race on); the contract fold, checks, and slug/subject
  parsing tested pure. The straggler gate, the terminal-slug
  refusals, and the clear-target path each have a live test whose
  failure mode is distinct.
