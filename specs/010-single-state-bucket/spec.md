# Spec 010 — one state bucket

**Work ID:** `008-single-state-bucket`
**Design:** `hits-hq` @ `700a296` —
[`02-DESIGN/ops-log.md`](../../../hits-hq/02-DESIGN/ops-log.md) § the
state projection; settled by decision
[`0012`](../../../hits-hq/03-DECISIONS/0012-single-state-bucket.md);
rooted in hits-hq issue
[`008-single-state-bucket`](../../../hits-hq/04-ISSUES/008-single-state-bucket/00-report.md).
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting
review and merge.

## What this delivers

Collapses the three KV buckets (`hits-items`, `hits-projects`,
`hits-meta`) into one, `hits-state`, its keyspace split by prefix:
`item.<id>` for snapshots, `project.<slug>` for the registry,
`system.<key>` for operational keys — today only `system.item-counter`.
Replay now derives the counter from the log, so the whole bucket is
disposable: delete it, boot, and snapshots, registry, and counter all
reproduce. A fleet's JetStream footprint halves — `hits-ops` and
`KV_hits-state` where four streams stood.

## Out of scope

- Deleting the old buckets: a new binary provisions `hits-state` and
  replays; `hits-items`/`hits-projects`/`hits-meta` linger until the
  operator removes them (`nats kv del …`). The release carrying this
  says so in its notes — no cleanup code (decision 0012).
- Per-prefix budgets or history — JetStream KV has neither, and 0005
  already rejected per-resource knobs.
- Reading per-key history (`get` with revisions) — still unimplemented,
  unchanged by this spec.

## Requirements

- **FR-01** One KV bucket, `hits-state`, declared once in `contract`,
  replaces the three bucket constants. Keys carry a kind prefix:
  `item.<id>`, `project.<slug>`, `system.<key>`; the item counter lives
  at `system.item-counter`. Prefixes are collision-free by the existing
  key shapes (decimal IDs, `[a-z0-9-]` slugs — no dots).
  (ops-log § the state projection)
- **FR-02** The bucket takes the items bucket's shape: per-key history
  10 and a byte budget of a quarter of the ops budget, scaling with
  `--max-bytes` exactly as `hits-items` did. The fixed 8 MiB
  small-bucket budgets retire. (decision 0012, amending 0005's shares)
- **FR-03** Replay derives the counter: after folding, the counter is at
  least the highest item ID the log names, and the raise never lowers
  it. Deleting the state bucket and restarting the node reproduces
  everything in it — snapshots, registry, and counter — and the next
  minted ID stays dense, colliding with nothing.
  (ops-log § identifiers; the strengthened FR-31)
- **FR-04** Project listing reads only `project.>` keys — filtered at
  the server, unaffected by however many item and system keys share the
  bucket. (services § hits-node)
- **FR-05** Tests reference the bucket through the `contract` constant —
  the hardcoded name strings go. The FR-31 test deletes the one state
  bucket (meta included, which the old test spared) and proves FR-03 by
  minting after replay; the fleet budget tests assert the merged shape
  on a max-bytes-required account.
