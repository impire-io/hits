# Spec 012 — search results render as a field table

**Work ID:** `18`
**Design:** `hits-hq` @ `9693f39` —
[`02-DESIGN/item-model.md`](../../../hits-hq/02-DESIGN/item-model.md)
§ properties (the column vocabulary) and
[`00-META/how-we-build.md`](../../../hits-hq/00-META/how-we-build.md)
§ headless ("human ergonomics are the job of the views built on top");
rooted in hits item `18`.
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting
review and merge.

## What this delivers

`hits search` resolves every hit to its item snapshot and renders a table
of item fields instead of bare `score  id` lines — a listing a human can
read without running one `hits get` per row. `--columns` selects the
columns (repeatable, comma-separated); by default every field populated
somewhere in the result set is a column, and columns empty in every row
are dropped. `--json` carries the full snapshots:
`{"hits": [{"id", "score", "item": {…}}], "total"}`.

## Out of scope

- The wire contract. The search service still replies `{id, score}` — the
  index is never authority (services.md § the index services; spec 002
  FR-04); the CLI resolves hits through the `get` endpoint like any other
  reader.
- `hits-mcp`. Replies pass through unchanged and composition stays the
  agent's job (mcp-server.md); `search_items` + `get_item` already
  compose.
- A batch-get endpoint. The fan-out is bounded and capped by the
  service's 100-hit page; a wire change waits for a need the cap cannot
  serve.
- `semantic` output — unchanged (the semantic index is down anyway,
  item 14).

## Requirements

- **FR-01** After a search, the CLI fetches each hit's snapshot via the
  client's `GetItem`, at most eight in flight, preserving hit order. A
  hit whose item is gone (a tombstone race) keeps its id and score with
  blank cells; any other failure fails the command rather than rendering
  a silently partial table.
- **FR-02** Human output is a header-plus-rows table followed by the
  existing `total: N` line. Column vocabulary and order: `id`, `score`,
  then the item fields in the snapshot renderer's order (`type` …
  `links`), named as item-model.md names them. Cells collapse
  whitespace, truncate past 60 runes with an ellipsis, and render empty
  as `-`.
- **FR-03** Default columns are the populated ones: `id`, `score`, and
  every column with a value in at least one row.
- **FR-04** `--columns` (repeatable, each value comma-separable) renders
  exactly the named columns in the given order — including columns empty
  in every row. An unknown name is an error before anything dials,
  listing the valid names.
- **FR-05** `--json` emits `{"hits": [{"id", "score", "item"}], "total"}`
  with the full `contract.Item` per hit; `--columns` does not shape
  JSON.
- **FR-06** Zero hits print only `total: 0` — no header for an empty
  table.

## Constitution check

- **One surface:** the CLI composes the existing `search` and `get`
  client endpoints; no side door, no service change.
- **Minimal build:** no new dependencies (stdlib tabwriter, stdlib
  concurrency); no batch endpoint ahead of a need.
- **The wire contract under test against real NATS:** the CLI tests
  drive `cli.Run` against an embedded server with the node and the
  search service live; nothing is mocked.

## Acceptance

`make check` green. `go test -race ./internal/cli/ -run 'Search|JSON'`
covers: the populated-column table with empty-everywhere columns
dropped, exact `--columns` selection and order (a forced empty column
included), pre-dial unknown-column rejection, and the enriched `--json`
shape decoding back into `contract.Item`.
