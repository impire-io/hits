# Plan 004 — hits-index-semantic

How [spec.md](spec.md) lands. Design authority: `hits-hq` @ `2c4d82d`.

## Layout

`cmd/hits-index-semantic` (thin main) over `internal/index/semantic`; wire
types and methods in `client`. Consumer and ready-gate are spec 002's
(filtered to `hits.ops.item.>`, backlog measured by subject-filtered stream
info).

## Mechanics

- **Embedder seam.** `Config{BaseURL, APIKey, Model}`; the store interface
  (upsert/remove/query) wraps chromem-go with
  `NewEmbeddingFuncOpenAICompat`. The binary reads base URL and model from
  flags, the key from `HITS_EMBEDDING_API_KEY`.
- **Document per item**: report plus note texts joined; a text-changing op
  (`created`, `noted`) re-embeds; other ops leave the vector alone;
  `tombstoned` deletes. chromem has no upsert — remove-then-add under the
  store's own lock.
- **Degraded, not down**: an embedding error logs and skips that item; the
  tail continues; the query endpoint still answers from what is embedded.
- **Query**: embed the text, return nearest IDs with similarity, limit
  capped at 100. chromem queries cannot return more results than the
  collection holds — the store clamps.

## Tests

Wire tests with a fake OpenAI-compatible `/v1/embeddings` httptest server
producing deterministic bag-of-words vectors — the real chromem HTTP path
runs, nothing external, NATS never mocked. Covers ranking, re-embedding on
note, tombstone, rebuild-before-wire, and one-item degradation (the fake
provider errors on a marker string).
