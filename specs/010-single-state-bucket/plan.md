# Plan 010 — one state bucket

How [spec.md](spec.md) lands. Design authority: `hits-hq` @ `700a296`.

## Layout

```
contract/names.go        StateBucket = "hits-state" replaces ItemsBucket,
                         ProjectsBucket, MetaBucket
internal/node/store.go   one KV handle (state) replaces items/projects/meta;
                         key prefixes (item./project./system.) as package
                         constants beside counterKey; openStore creates one
                         bucket; load/save prepend prefixes; listProjects
                         uses ListKeysFiltered("project.>") and strips the
                         prefix; replay tracks the highest item ID it folds
                         and raises the counter after the fold
internal/node/config.go  stateMaxBytes (a quarter of ops) replaces
                         itemsMaxBytes; smallBucketMaxBytes retires
client/items_test.go     the FR-31 test deletes contract.StateBucket (one
                         bucket, counter included) and proves the derived
                         counter by minting the next dense ID after replay
internal/fleet/fleet_test.go  budget assertions move to the merged shape:
                         KV_hits-state at ops/4, no small buckets
```

## Mechanics

- **The prefixes stay inside `internal/node`.** Only the store touches
  keys; `contract` carries the bucket name because tests and operators
  need it, but the key layout has one consumer — minimal build.
- **The counter raise is CAS, monotone, and last.** Replay folds first,
  then lifts `system.item-counter` to the observed maximum via the same
  create-or-update-at-revision loop `mintID` uses; a concurrent mint
  that got there first wins (`cur >= n` returns). Item ops are every op
  that is not `registered`; their `entity` is the decimal ID.
- **Migration is replay itself.** `CreateOrUpdateKeyValue` provisions
  `hits-state` on first boot and `replay` fills it from `hits-ops` —
  including the counter, so nothing is carried over from `hits-meta`.
  The one accepted edge (decision 0012): an ID minted for an op that
  never landed is reissued after a rebuild; the log never named it.

## Tests

- `TestReplayReproducesProjections` deletes `contract.StateBucket` —
  strictly stronger than the old items+projects delete, since the
  counter now goes down with the bucket — and, after replay, asserts
  the snapshot and registry match and a fresh create mints the next
  dense ID rather than colliding with the replayed item.
- `TestMaxBytesRequired`/`TestMaxBytesOverride` assert `KV_hits-state`
  at a quarter of the ops budget (default and 64 MiB override) and no
  longer expect the 8 MiB buckets.
- Everything else holds by construction: handlers reach state only
  through the store, and the wire contract is untouched.
