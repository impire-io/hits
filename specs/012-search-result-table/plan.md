# Plan 012 — search results render as a field table

## Layout

```
internal/cli/query.go     runSearch grows --columns and the bounded get
                          fan-out (fetchRows)
internal/cli/print.go     printSearch becomes printSearchTable: the column
                          registry, parseColumns, populatedColumns, cell,
                          and the enriched --json shape
internal/cli/cli_test.go  TestSearchCommand extended; TestSearchColumns,
                          TestSearchUnknownColumn, TestSearchJSON added
```

## Mechanics

- `searchRow{ID, Score, Item *contract.Item}` pairs one hit with its
  snapshot; `fetchRows` resolves hits through `client.GetItem` behind an
  eight-slot semaphore, order-preserving. A `not-found` (tombstone race)
  degrades that row to id and score; any other error joins and fails the
  command — no silently partial tables.
- The column registry is an ordered `[]col{name, extract}`: `id` and
  `score` read the hit, every other column reads the snapshot through a
  nil-safe lift. Cell text reuses `landString` and `fixRefString`; dates
  render day-precision.
- `parseColumns` resolves names against the registry **before**
  `inv.dial()`, so a typo never costs a connection.
- JSON mode wraps the rows as `enrichedReply{hits, total}` — a
  presentation type of this view; `client.SearchReply` and the wire are
  untouched.

## Tests

All against the embedded real-NATS harness (`startStore` plus
`search.Start` per test), race detector on: the table shape with
empty-everywhere columns dropped, exact column selection and ordering,
guarded pre-dial validation (`guardConnector`), and the `--json` shape
decoding into `contract.Item`.
