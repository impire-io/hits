# Plan 001 — Item store core

How [spec.md](spec.md) lands in this repo. Design authority:
`hits-hq` @ `826c108`.

## Package layout

```
contract/            public — op envelope, op catalog, item & project model,
                     invariant checks, pure fold. Imports nothing of ours.
client/              public — wire contract (subjects, request/reply types)
                     and caller methods. Imports contract only.
internal/node/       the hits-node service: store bootstrap, write path,
                     projections, endpoints. Imports contract + client.
internal/natstest/   embedded NATS for tests; gains a JetStream variant.
```

Future indexer services get their own `internal/` trees importing `contract`
(and `client` if they call the API) — never `internal/node`. Enforced by
depguard (FR-02).

## The write path (FR-10..12, 20, 21)

One code path for every command:

1. Load the item snapshot from `hits-items` (`not-found` if absent where
   required). If the stream's last op for the subject is newer than the
   snapshot (crash between publish and fold), catch up first: ordered
   consumer filtered to the item's subject from `snapshot.seq+1`, folding
   into KV.
2. `contract.Check` the command op against the snapshot — every rejection is
   an `InvariantError` whose name goes to the caller verbatim.
3. Publish the op to `hits.ops.item.<id>` with
   `WithExpectLastSequencePerSubject(snapshot.seq)` and `WithMsgID(op.id)`.
   CAS conflict (API codes 10071/10164) → re-read and retry, bounded.
4. Fold the op into the snapshot (`contract.Apply`) and write it to KV at
   the expected revision; a revision conflict means another instance folded
   first — re-read and keep the newer.

`create` publishes with expected sequence 0 (new subject); IDs come from a
CAS-update loop on the `item-counter` key in `hits-meta`.
`project.register` is the same shape on `hits.ops.project.<slug>` with
expected sequence 0 — a conflict is `already-registered`.

## Projections (FR-30, 31)

Snapshots carry the stream sequence of the last op folded in; folding skips
ops at or below it, which makes replay idempotent. `Replay` walks the whole
stream with an ordered consumer (`FetchNoWait` until drained) and folds
every op — `node.Start` runs it once at boot to heal gaps, and the FR-31
test deletes the buckets and proves equality after replay.

## Contract semantics settled here (within the design's letter)

- Transition table: `open → diagnosing|located|resolved|wontfix`,
  `diagnosing → located|resolved|wontfix`, `located → resolved|wontfix`;
  tasks skip the localization stages (`open → resolved|wontfix`).
  While blocked, no transitions — unblock first. Terminal accepts none.
- `noted` and `linked`/`unlinked` stay legal on closed items (the corpus is
  memory; cross-references accrue after closure). `edited`, claims, blocks,
  and transitions do not.
- Actor handles: `^[a-z][a-z0-9-]{0,63}$`. Project slugs:
  `^[a-z0-9][a-z0-9-]{0,63}$`.
- Timestamps: envelope `at` is RFC 3339; the `closed` property is the
  civil date (UTC) of the closing op, per the design's "date, on either
  terminal status".

## Tests

- `contract`: table-driven unit tests over transitions and invariants (pure
  Go, no NATS).
- `client` ↔ `internal/node`: wire tests against the embedded JetStream
  server — the spec's acceptance list, each as its own test, plus a
  round-trip covering every endpoint.

## Out of plan

Indexers, CLI verbs, constitution scaffolding — per the spec's
out-of-scope list.
