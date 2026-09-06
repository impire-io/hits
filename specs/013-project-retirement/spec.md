# Spec 013 — projects retire

**Work ID:** `15`
**Design:** `hits-hq` @ `de2f701` —
[`03-DECISIONS/0015-project-retirement.md`](../../../hits-hq/03-DECISIONS/0015-project-retirement.md),
[`02-DESIGN/ops-log.md`](../../../hits-hq/02-DESIGN/ops-log.md) § op catalog,
[`02-DESIGN/item-model.md`](../../../hits-hq/02-DESIGN/item-model.md)
§ projects and actors,
[`02-DESIGN/services.md`](../../../hits-hq/02-DESIGN/services.md) and
[`02-DESIGN/mcp-server.md`](../../../hits-hq/02-DESIGN/mcp-server.md)
(the surface); rooted in hits item `15`.
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting
review and merge.

## What this delivers

A project can leave the `located-in` vocabulary: a `retired` op on
`hits.ops.project.<slug>` with a required reason. After it, `project.list`
drops the slug, every op that writes `located-in` refuses it
(`retired-project`), and the slug can never be registered again — the
existing expected-sequence-zero CAS enforces that without new machinery.
History is untouched: items already naming the slug keep it and still
close. The surface grows in lockstep: `hits.api.project.retire`,
`client.RetireProject`, `hits project retire <slug> --reason <r>`, and the
`retire_project` MCP tool.

## Out of scope

- A rename verb, an un-retire op, or slug reuse — rejected by decision
  0015; the path is register the successor, re-point open items, retire
  the old slug.
- A guard against retiring a slug still referenced by items — needs an
  item scan the write path does not have, to protect a state that stays
  fully workable.
- Any change to the graph index's responses: project nodes materialize
  only through item edges, so an unreferenced retired project already
  appears nowhere. The graph fold merely learns to skip the new op
  instead of logging a decode error.
- Retiring `001-hits` in the live install — the validation act after this
  merges and the fleet redeploys, not part of the build.

## Requirements

- **FR-01** The contract gains `OpRetired` ("retired") with a payload of
  one required reason, budgeted as a label (1 KiB, decision 0014).
- **FR-02** `Project` carries `Retired bool` and `RetireReason`;
  `ApplyProject` folds the op onto the kept registry key, preserving
  idempotent replay (an op at or below the snapshot's Seq is skipped).
- **FR-03** `CheckProjectOp` accepts `retired` only for a registered,
  not-yet-retired slug with a non-empty reason; registering over a
  retired slug is refused with a message naming retirement, and every
  other op stays refused as before.
- **FR-04** `hits-node` serves `hits.api.project.retire`: validate
  against the registry snapshot, publish CAS-guarded on the project's
  subject, fold into the state bucket. Replay (`foldOne`) routes the op
  to the project fold, never the item fold.
- **FR-05** `located-in` validation (`checkRegistered`) refuses retired
  slugs with the `retired-project` invariant; `project.list` drops
  retired entries.
- **FR-06** `client.RetireProject` round-trips the endpoint; the CLI
  grows `hits project retire <slug> --reason <r>` (reason required); the
  MCP server grows the `retire_project` tool — one tool per endpoint,
  the 1:1 rule.
- **FR-07** All of it tested against real NATS via the embedded harness,
  race detector on; the contract fold and checks tested pure.
