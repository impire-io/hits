# Spec 002 — hits-index-search

**Work ID:** `002-hits-index-search`
**Design:** `hits-hq` @ `9f7c9b2` —
[`02-DESIGN/services.md`](../../../hits-hq/02-DESIGN/services.md) § the index
services, [`02-DESIGN/ops-log.md`](../../../hits-hq/02-DESIGN/ops-log.md)
§ ordering; settled by decision
[`0001`](../../../hits-hq/03-DECISIONS/0001-item-store-architecture.md).
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting review
and merge.

## What this delivers

The first index service: `hits-index-search`, a separate binary exposing the
**`hits-search`** micro service. It consumes the ops-log through its own
ordered consumer, folds item state with the shared contract fold, maintains
an embedded Bleve full-text index rebuilt from sequence 1 on boot, and
answers `hits.search.query` — full-text over reports and notes, filterable
by type and status, paged. This unlocks the duplicate-check workflow: the
kept-forever corpus becomes searchable, resolved items included.

## Out of scope (own specs later)

- `hits-index-graph` and `hits-index-semantic`.
- Index persistence and external stores — the store sits behind an
  interface; rebuild-on-boot is the deliberate default (decision 0001).
- CLI verbs; any write path — this service writes nothing, anywhere.

## Requirements

- **FR-01** A separate binary `cmd/hits-index-search`, its logic in its own
  package tree importing `contract` (and `client` for the wire types) but
  never `internal/node`; `internal/node` never imports it. Both directions
  depguard-enforced. (services § code layout)
- **FR-02** The service consumes `hits.ops.item.>` via an ordered consumer —
  in-order, gap-detected, single-instance — folding each op with
  `contract.Apply`. On boot it replays from sequence 1 and registers its
  endpoints only once caught up to the stream head observed at start; until
  then it is not on the wire. (ops-log § ordering; services § index
  services)
- **FR-03** The index holds, per live item: report and note text
  (full-text), and type, status, priority, located-in (exact-match).
  Tombstoned items are dropped from the index — projections drop them from
  every view. (item-model § tombstone)
- **FR-04** `hits.search.query` accepts a query string (optional), type and
  status filters (optional), and limit/offset paging; it returns matching
  item IDs with scores, plus the total match count. Every hit resolves to an
  item ID — state comes from the `hits` service, never from the index.
  (services § index services)
- **FR-05** The Bleve index is embedded, in-memory, pure Go, and sits behind
  a small store interface so an external store can slot in later without
  touching the consumer or the endpoint. (decision 0001)
- **FR-06** The wire contract (subject, request/reply types) is declared in
  the `client` package with a caller method, like every other endpoint.
  (AGENTS.md: one surface)

## Constitution check

Against `AGENTS.md` and `how-we-build.md`: reads only projections of the
ops-log, never authority (FR-03/04); a NATS micro service with the standard
discovery surface (FR-02); one surface through `client` (FR-06); minimal
build — no persistence knob, no external store config, non-goals stated
(out-of-scope list); wire contract tested against real NATS. No conflicts.

## Acceptance

- `make check` green; no skipped tests; race detector on.
- Wire tests against the embedded JetStream server, node and search service
  both live, never a mocked NATS client:
  - items created through `hits.api` become findable by report and by note
    text (live tail, FR-02);
  - a search service started *after* items exist finds them (rebuild on
    boot, FR-02);
  - type and status filters narrow results; paging bounds them (FR-04);
  - a tombstoned item disappears from results (FR-03);
  - the service is absent from the wire until caught up (FR-02).
