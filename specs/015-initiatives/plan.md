# Plan 015 — initiatives

## Layout

```
contract/model.go        Initiative; Project.Initiative; Item.Initiative
contract/ops.go          OpAssigned; AssignedPayload; initiative fields
                         on Created/Edited/Registered payloads
contract/check.go        ValidInitiativeSlug, ParseItemID;
                         CheckInitiativeOp; created/edited/project
                         checks grow the 0016 invariants
contract/apply.go        folds for the new fields and ops;
                         ApplyInitiative
contract/names.go        InitiativeOpsPrefix
client/items.go          subjects, request types, methods for
                         initiative.* and project.assign; initiative on
                         create/edit
client/search.go         SearchRequest.Initiative
client/graph.go          NodeInitiative, EdgeInInitiative
internal/node/store.go   per-initiative counters; subject-routed
                         foldOne/foldRange/replay; initiative
                         register/retire/list; assignProject;
                         checkRegistered learns the initiative match;
                         checkInitiativeLive
internal/node/handlers.go create resolves+validates initiative before
                         minting; edit's initiative; new handlers
internal/node/node.go    endpoint table
internal/index/search/   initiative keyword field + query term
internal/index/graph/    subject-aware fold; folded project state;
                         initiative nodes and in-initiative edges;
                         prefixed blocked-by refs
internal/connect/config.go Defaults.Initiative; DefaultInitiative;
                         SaveDefaultInitiative
internal/cli/initiatives.go register|retire|list|select
internal/cli/items.go    create --initiative (flag → env → selected)
internal/cli/projects.go register --initiative; assign
internal/cli/query.go    search --initiative
internal/cli/audit.go    legacy + per-initiative walk; --initiative;
                         prefixed merge scan
internal/cli/print.go    initiative printers; project shows initiative
internal/mcp/            four new tools; initiative on create/edit/
                         search inputs; startup default initiative
```

## Mechanics

- **Dispatch is the subject, not the op type.** `registered`/`retired`
  appear on project and initiative subjects alike, so `foldRange`
  hands its callback the subject and `foldOne` routes on the prefix —
  exactly the design's stated consumer rule. The graph consumer does
  the same, and additionally folds project registry state (with
  `ApplyProject`) so `assigned` moves re-derive the project's
  `in-initiative` edge.
- **Minting stays validate-then-mint.** The create handler checks the
  initiative is registered and live and `located-in` matches it
  *before* minting from `system.item-counter.<slug>`, so a rejected
  create burns no number. The minted ID's prefix and the payload's
  echoed initiative agree by construction.
- **The initiative match is lenient exactly once.** `located-in`
  versus item initiative compares only when both sides carry a value —
  the pre-backfill window where legacy items and projects have none
  must keep working. New writes always carry both.
- **The audit walks ranges it can enumerate.** Legacy: bare `1..gap`.
  Per initiative (from `initiative.list`): `<slug>-1..gap`. The list
  drops retired initiatives, so a retired initiative's items leave the
  audit's reach — an accepted boundary, revisited when the first
  initiative actually retires; legacy plus live covers the whole
  corpus today.
- **Config writes stay schema-preserving.** `SaveDefaultInitiative`
  mirrors `saveDefaultContext`: read the document as a map, set
  `defaults.initiative`, write back 0600.

## Tests

Contract (pure): ParseItemID and ValidInitiativeSlug tables (bare,
prefixed, hyphenated slugs, trailing-digit refusals); fold and check
tables for the new ops and fields; initiative-mismatch, immutability,
and pre-backfill leniency cases. Client/node (embedded NATS, race on):
initiative lifecycle end to end — register, create mints `slug-1`,
dense per-initiative sequences, list, retire refuses new creates,
project assign moves the edge, legacy-style edit assignment refused on
prefixed items. Search: initiative filter narrows. Graph: initiative
nodes, in-initiative edges from both derivations, prefixed blocked-by.
CLI: the new verbs; create's flag → env → selected-default resolution
(config in a temp XDG home) and the no-initiative refusal; audit over
mixed legacy and prefixed corpora with prefixed merge subjects. MCP:
new tools present with annotations; create honors the startup default.
