# Plan 016 — release targeting

## Layout

```
contract/model.go        Release; Item.Target
contract/ops.go          OpShipped; ShippedPayload; target fields on
                         Created/Edited payloads
contract/check.go        ValidReleaseSlug, ParseReleaseEntity;
                         CheckReleaseOp; created/edited checks grow
                         the target form check
contract/apply.go        ApplyRelease; target folds on created/edited
contract/names.go        ReleaseOpsPrefix
client/items.go          subjects, request types, methods for
                         release.register/ship/retire/list; target on
                         create/edit
client/search.go         SearchRequest.Target
client/graph.go          NodeRelease, EdgeTargets
internal/node/store.go   release. KV keys; load/save/register/ship/
                         retire/list; checkReleaseLive; the straggler
                         scan; foldOne routes the release subject
internal/node/handlers.go create/edit validate target against the
                         registry; four new handlers
internal/node/node.go    four endpoint table rows
internal/index/search/   target keyword field + query term
internal/index/graph/    release nodes named on registration;
                         item → release targets edge derivation
internal/cli/releases.go register|ship|retire|list
internal/cli/items.go    create --target; edit --target (clearable)
internal/cli/query.go    search --target
internal/cli/print.go    release printers
internal/cli/audit.go    target invariants netted in the walk
internal/mcp/            four new tools; target on create/edit/search
                         inputs
```

## Mechanics

- **Dispatch is the subject, as 015 settled.** `registered`/`retired`
  reuse the shared op types; `shipped` is new and release-only.
  `foldOne` and the graph fold grow a `ReleaseOpsPrefix` branch.
- **The entity is `<initiative>.<slug>`, parsed not guessed.**
  Release slugs allow dots (versions), initiative slugs never carry
  one, so `ParseReleaseEntity` splits on the first dot; the CAS scope
  and the per-initiative uniqueness scope are the same subject. No
  per-slug counters, no trailing-digit rule — those are ID-prefix
  machinery, and releases prefix nothing.
- **`target` is a bare slug, registry-checked at the node.** The
  contract checks form only (`ValidReleaseSlug` when non-empty); the
  node validates create/edit targets against
  `release.<item-initiative>.<slug>` — live releases only
  (`unknown-release`, `release-shipped`, `release-retired`), the
  exact seam where `located-in` is checked today. `EditedPayload.
  Target` is a pointer: nil untouched, empty clears — the clear is
  the straggler triage's tool.
- **The straggler gate is a scan, not an index.** `ship` lists item
  snapshots and refuses (`open-targets`, offenders named) while any
  non-terminal item of the release's initiative targets the slug.
  Shipping is rare; the scan is the minimal build. Tombstoned and
  terminal items hold nothing hostage.
- **Shipping completes, retirement removes.** `release.list` (per
  initiative) drops retired slugs — they left the vocabulary — and
  keeps shipped ones with their refs: the shipped list is release
  history, the unshipped remainder is the working vocabulary. Both
  are terminal to further ops, and the subject CAS makes either
  permanent against re-registration.
- **The audit nets, write-time enforces.** Beside the ref checks, the
  per-initiative walk lists releases once and flags: a target naming
  no known release (`target-unknown`), a non-terminal item targeting
  a terminal release (`target-terminal`) — the stray that guardless
  retirement deliberately permits, surfaced where the periodic net
  runs. No new pass, no new surface.

## Tests

Contract (pure): ValidReleaseSlug and ParseReleaseEntity tables
(dotted versions, empty segments, uppercase, initiative split); fold
and check tables for register/ship/retire (already-registered,
empty-refs, already-shipped, release-retired, release-shipped,
empty-reason, budgets); target applied from created/edited, cleared by
pointer-to-empty; terminal items refuse a target edit. Client/node
(embedded NATS, race on): the release lifecycle end to end — register,
target at create and by edit, clear, `ship` refused `open-targets`
with a straggler then succeeding after re-target, target writes
refused against shipped and retired slugs, re-registration refused,
list showing shipped-with-refs and dropping retired. Search: target
filter narrows. Graph: release nodes named, targets edge derived and
dropped on clear. CLI: the release verbs; create/edit/search target
flags including the explicit clear. Audit: a retired release with a
straggler yields `target-terminal`; an unknown target yields
`target-unknown`. MCP: four tools present with annotations, tool-name
and read-only lists updated, target honored on create.
