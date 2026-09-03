# Spec 004 — hits-index-semantic

**Work ID:** `004-hits-index-semantic`
**Design:** `hits-hq` @ `2c4d82d` —
[`02-DESIGN/services.md`](../../../hits-hq/02-DESIGN/services.md) § the index
services ("embeddings"); decision
[`0001`](../../../hits-hq/03-DECISIONS/0001-item-store-architecture.md).
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting review
and merge.

## What this delivers

`hits-index-semantic`: a separate binary exposing the **`hits-semantic`**
micro service — nearest-items-to-a-text over chromem-go, with vectors
produced by a configurable OpenAI-API-compatible provider (base URL, API
key, model). The provider is the fleet's one external dependency; its
outages degrade semantic search only, and every other service runs without
it.

## Requirements

- **FR-01** Separate binary `cmd/hits-index-semantic`, logic in
  `internal/index/semantic`, bound by the existing depguard rules.
  (services § code layout)
- **FR-02** Ordered consumer over `hits.ops.item.>`, shared contract fold,
  on the wire only once caught up (spec 002's mechanics). Each live item's
  report and notes are embedded as one document, re-embedded when its text
  changes; tombstoned items leave the collection. (services § index
  services; item-model § tombstone)
- **FR-03** The provider is configured, never hardwired: base URL and model
  by flag, the API key only from the environment
  (`HITS_EMBEDDING_API_KEY`) — a secret does not belong in argv. The
  embedding store (chromem-go, pure Go, in-memory) sits behind the same
  store-interface seam as the other indexers. (decision 0001; services §
  embeddings)
- **FR-04** A failed embedding call skips that item with a log line and
  never stops the tail — degraded, not down. (services § embeddings)
- **FR-05** `hits.semantic.query` embeds the query text and returns the
  nearest item IDs with similarity scores, count capped. Wire contract and
  caller method in `client`. (one surface)

## Constitution check

Per specs 001/002: projection-only, micro service, one surface, wire tests
against real NATS. The external provider is the design's own explicit
exception, config'd to the minimum (three values). No conflicts.

## Acceptance

- `make check` green; race detector on; no skipped tests.
- Wire tests run node + semantic service on embedded JetStream against a
  **fake OpenAI-compatible provider** (an httptest server producing
  deterministic bag-of-words vectors) — the real HTTP embedding path is
  exercised, no external calls, nothing mocked at the NATS layer:
  - semantically close text ranks the right item first (live tail);
  - re-embedding after a note changes the ranking;
  - a tombstoned item leaves the results;
  - rebuild-on-boot embeds the pre-existing corpus before going on the
    wire;
  - a provider that errors for one item degrades that item only (FR-04).
