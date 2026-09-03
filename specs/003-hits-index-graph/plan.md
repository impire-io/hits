# Plan 003 — hits-index-graph

How [spec.md](spec.md) lands. Design authority: `hits-hq` @ `2c4d82d`.

## Layout

`cmd/hits-index-graph` (thin main) over `internal/index/graph`; wire types
and methods in `client`. The consumer/ready-gate shape is spec 002's, with
one difference: the consumer is unfiltered (`hits.ops.>`) because project
registrations feed project-node names, so the backlog measure is the
stream's total message count.

## Mechanics

- **The graph is a function of the snapshots.** The fold keeps
  `map[id]*contract.Item` and `map[slug]*contract.Project`; after each op,
  the touched item's full outgoing edge set is *recomputed* from its
  snapshot and set-replaced in the store — asserted links from `Links`,
  derived edges from `LocatedIn`, `Reporter`, `Claim`, and an all-digits
  `BlockedBy` while blocked. No incremental edge bookkeeping, so replay and
  live tail cannot diverge.
- **Store interface**: set-edges / remove-node / neighbors / nodes-by-ref,
  implemented as in-memory adjacency (out-map plus reverse in-map) under a
  mutex — queries run on micro handler goroutines while the fold writes.
  Tombstoned items are removed outright; the reverse map purges their
  in-edges.
- **Endpoints**: `neighbors` filters by direction and edge types; `walk`
  is BFS with a depth cap of 5 and a visited set, returning nodes and the
  edges that reached them. Node refs are `{kind, id, name?}` — name only
  on project nodes, from the registry fold.

## Tests

Wire tests in `client` (harness of spec 002): each derived-edge lifetime,
the project-neighbors query, walk depth and filters, tombstone removal,
rebuild-on-boot equivalence.
