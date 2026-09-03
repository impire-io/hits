# Spec 003 — hits-index-graph

**Work ID:** `003-hits-index-graph`
**Design:** `hits-hq` @ `2c4d82d` —
[`02-DESIGN/services.md`](../../../hits-hq/02-DESIGN/services.md) § the index
services ("the graph is wider than the links"),
[`02-DESIGN/item-model.md`](../../../hits-hq/02-DESIGN/item-model.md) § links;
decisions [`0001`](../../../hits-hq/03-DECISIONS/0001-item-store-architecture.md)
and [`0002`](../../../hits-hq/03-DECISIONS/0002-projects-and-actors.md).
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting review
and merge.

## What this delivers

`hits-index-graph`: a separate binary exposing the **`hits-graph`** micro
service — link exploration over an in-memory adjacency projection of the
ops-log, wider than the asserted links: it materializes project and actor
nodes and derives edges from properties and ops.

## Graph contract

Nodes are `item:<id>`, `project:<slug>` (carrying the registered display
name), `actor:<handle>` (existing only by reference). Edges:

| Edge | Source | Lifetime |
|---|---|---|
| `duplicates`, `relates-to` | `linked`/`unlinked` ops | until retracted |
| `located-in` → project | the property | while set |
| `reported-by` → actor | the `created` op's actor | forever |
| `claimed-by` → actor | the current claim | while claimed |
| `blocked-by` → item | a `blocked` op whose `blocked-by` is exactly an item ID (all digits) | while the block stands |

A `blocked-by` of prose derives no edge. Edges to items not yet seen are
held as-is — consumers tolerate dangling refs by design. A tombstoned item
disappears: its node, its edges, and every edge pointing at it leave all
results.

## Requirements

- **FR-01** Separate binary `cmd/hits-index-graph`, logic in
  `internal/index/graph`, importing `contract`/`client` only —
  the existing depguard rules already bind it. (services § code layout)
- **FR-02** One ordered consumer over **all** of `hits.ops.>` (project
  registrations included — project nodes need their names), folding with
  the shared contract fold; on the wire only once caught up with the
  backlog measured at start, per spec 002's mechanics. (ops-log § ordering)
- **FR-03** The adjacency store is in-memory behind a store interface;
  the whole graph is recomputed from item snapshots, so replay and live
  tail share one code path. (decision 0001)
- **FR-04** `hits.graph.neighbors`: the edges at one node, filterable by
  edge type and direction (in/out/both). `hits.graph.walk`: bounded-depth
  breadth-first expansion from one node, same filters, depth capped.
  Results carry typed node refs; project nodes carry their names.
  (services § index services)
- **FR-05** Wire contract and caller methods in `client`. (one surface)

## Constitution check

Per specs 001/002: projection-only, micro service, one surface, minimal
build (no persistence, no external store config), wire tests against real
NATS. No conflicts.

## Acceptance

- `make check` green; race detector on; no skipped tests.
- Wire tests, node + graph service live on embedded JetStream:
  - asserted links appear as edges and vanish on unlink;
  - `located-in`, `reported-by`, and `claimed-by` edges derive from ops,
    and the claim edge follows release and steal;
  - a block on an item ID derives a `blocked-by` edge that drops on
    unblock; a prose blocker derives nothing;
  - neighbors of a *project* node list every item located in it — the
    design's motivating query;
  - walk expands to depth over mixed edge kinds and respects the cap;
  - a tombstoned item leaves every result;
  - rebuild-on-boot: a service started after the corpus exists answers
    identically (project names included).
