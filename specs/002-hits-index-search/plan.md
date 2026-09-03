# Plan 002 — hits-index-search

How [spec.md](spec.md) lands. Design authority: `hits-hq` @ `9f7c9b2`.

## Layout

```
cmd/hits-index-search/    thin main, mirroring cmd/hits-node
internal/index/search/    the service: consumer, fold, index, endpoints
client/                   gains the hits-search wire contract + method
```

depguard grows both directions of the service boundary: `internal/node`
cannot import `internal/index`, and `internal/index` cannot import
`internal/node`. Services share only `contract` (and the `client` surface).

## Mechanics

- **One ordered consumer, one goroutine.** `Start` measures the stream head,
  then `Consume`s `hits.ops.item.>` from sequence 1. Each op folds into an
  in-memory `map[id]*contract.Item` via `contract.Apply` and re-indexes that
  item (or deletes it, once tombstoned). When the fold passes the head
  measured at start, a ready channel closes and `Start` registers the micro
  service — before that the service does not exist on the wire. The consumer
  keeps running for the live tail; ordered-consumer gap recovery re-delivers
  in order, and the fold's Seq idempotence absorbs any overlap.
- **Index behind an interface.** `indexer` (upsert, delete, query) with the
  one implementation: Bleve v2, `NewMemOnly`, custom mapping — `report` and
  `notes` through the standard analyzer; `type`, `status`, `priority`,
  `located-in` as keyword fields for exact filtering.
- **Query shape.** Free text → match query over the text fields; filters →
  term queries; all conjoined; empty text + no filters → match-all. Results
  carry ID, score, and the total count; limit defaults sane, offset pages.
- **Errors** follow the platform convention: machine-legible codes
  (`invalid-request`, `internal`).

## Tests

Wire tests in `client`, running node + search service against embedded
JetStream: live-tail visibility (with a bounded poll for the async index),
rebuild-on-boot, filters and paging, tombstone removal. Bleve mapping
subtleties (keyword vs text fields) are covered by the same wire tests —
no unit tests against the index internals, the wire behavior is the
contract.
