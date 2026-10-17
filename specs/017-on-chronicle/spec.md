# Spec 017 — HITS on chronicle: the client on chronicle's client, the fleet gone

**Work ID:** `hits-7` (tracker item hits-7; the build of hits-hq decisions 0020, 0021 and 0022, targeting release 0.8.0)
**Design:** `hits-hq` @ `b9acfb3` (hq PR #15) —
[`02-DESIGN/the-store.md`](../../../hits-hq/02-DESIGN/the-store.md),
[`02-DESIGN/the-client.md`](../../../hits-hq/02-DESIGN/the-client.md),
[`02-DESIGN/the-cutover.md`](../../../hits-hq/02-DESIGN/the-cutover.md),
and the amended [`02-DESIGN/item-model.md`](../../../hits-hq/02-DESIGN/item-model.md) and
[`02-DESIGN/mcp-server.md`](../../../hits-hq/02-DESIGN/mcp-server.md);
out of research [`005-hits-on-chronicle`](../../../hits-hq/01-RESEARCH/005-hits-on-chronicle/00-overview.md)
(the mapping, the spike, the replay), under decisions [0018](../../../hits-hq/03-DECISIONS/0018-hits-runs-on-chronicle.md) and
[0019](../../../hits-hq/03-DECISIONS/0019-the-writer-is-the-client-the-store-does-not-require-verified-authors-and-a-release-lives-under-its-initiative.md).
**Status:** built on branch `hits-7` ([plan.md](plan.md)).

## What this delivers

HITS keeps its model and sheds its substrate. Release **0.8.0**: nothing of HITS runs as a service; a HITS is one store in a chronicle account, and `hits` and `hits-mcp` are clients of it.

1. **The store** (`client/store.go`, the-store.md). `TypeDeclarations` and `IndexDeclarations` are the four types — `release`, `initiative` (with `releases` as a child), `project`, `item` — and `solution`, each operation with its effect and the precondition that encodes its invariant (chronicle-hq decision 0059), the item schema stating the shapes every state keeps, and the three indexes: `text` (search over the history's `create` and `note`), `meaning` (semantic over the same), `rel` (the graph from the fields that hold paths). `client.Init` applies them create-if-absent and writes the announcement `solution/hits`; `client.Manifest` is the same declaration as the install manifest (chronicle-hq decision 0054), generated at release time into the archive as `install.json`.
2. **The mapping** (`contract/state.go`, the-store.md § the types, § references as paths). Pure functions between the model and chronicle's state: paths (`item/<id>`, `project/<slug>`, `initiative/<slug>`, `initiative/<slug>/releases/<slug with . as _>`, `member/<handle>`), `ItemState` and `ItemFromState` and their vocabulary siblings, `Diff` — the difference a write sends — and `ItemOperation`, which names an op's operation, a claim with a steal as `steal`.
3. **The client** (`client/`, the-client.md). The same request types and methods, without an actor: the author is the principal the chronicle client connected as. A write is the loop — read the instance exactly (the bucket's state, then every operation after it folded with chronicle's own fold, so a read never trails the write and the guard is the last operation's), check with `contract.CheckOp` and the cross-instance checks the fleet made, fold with `contract.Apply`, send the difference through chronicle's node under the expected sequence with the op's id as the message id; on a guard miss, or a precondition chronicle's fresh pre-flight refused, read again. A create is a mint on the initiative's counter (`next`, under the guard) then a create with the create-only guard. Reads are chronicle's: state and the trail from history (`GetItem`), listings (`ListItems`, the vocabularies, `ListReleases` under its initiative), the three indexes with the client's filters (`SearchItems` joins hits with state; an empty query is the listing), `SemanticSearch`, `GraphNeighbors` and `GraphWalk` with the graph's own node ids. Refusals keep the invariants' names in `APIError`; a missing instance is `not-found`; a rule chronicle refused is `refused`.
4. **The connection** (`internal/dial`). A chronicle context — `chronicle login` for the hosted account, `chronicle context add` for a credential on any NATS — loaded and dialled as chronicle's CLI dials it (`cli.LoadContext`, `Context.Dial`); the store is the context's selection or `hits`. `--context` on every sentence; `CHRONICLE_CONTEXT` the fallback.
5. **The CLI** (`internal/cli`). Every item verb, the three vocabularies, `search`, `semantic`, `graph`, `audit` and `version` unchanged in their words; `--actor` and `HITS_ACTOR` gone; `up`, `context`, `auth` and `ping` gone. New: `store init [--version]` and `store manifest`; `migrate --from <hits context>` for one release. The audit reads every item from the listing and gains two reads: `numbering-gap` (warn) per initiative, and `unverified-write` (fail) after `--since-seq`, the cutover's sequence. The selected initiative lives in `internal/config` (`hits initiative select`, as before).
6. **The MCP server** (`internal/mcp`). The same tools over the same client; the actor flag is gone; `--context` and `--initiative` remain.
7. **The migration** (`internal/cli/migrate.go`, the-cutover.md). Reads `hits-ops` from sequence 1 through a hits context (`internal/connect`, kept for this verb alone), writes every op as an operation on its mapped path with its id, actor and time on the record, mints the counters last, and verifies: every entity's state equal to the install's own fold, every note's author and time equal, no operation marked — the research's replay, as a verb.
8. **The repository.** `internal/node`, `internal/index`, `internal/fleet`, the four fleet mains and the hits-context verbs are removed; `internal/natstest` stays for `internal/connect`'s tests. The tests boot chronicle's quick start in-process (`internal/chronicletest`): the real wire, never a mock. The release ships `hits`, `hits-mcp` and `install.json`.

## Constitution check

Against `AGENTS.md` and `hits-hq/00-META/how-we-build.md` as restated by the graduation:

- **The history is the record, and chronicle keeps it.** No stream, bucket or index of HITS's own; every read is chronicle's, every write an operation under the guard.
- **HITS runs no service of its own; every surface is a client.** `cmd/hits` and `cmd/hits-mcp` over `client` and `contract`; depguard keeps the adapters on the client and the contract a leaf.
- **Tested against real NATS.** Every test runs over chronicle's embedded quick start.
- **One surface.** Everything callable goes through the `client` package; the migration's bare publish is the one direct write, and it is the importer's by design (decision 0019 point 3).
- **Minimal build.** No dual-write, no second counter, no filter on the wire, no `migrate` beyond this release.

## What it does not do

No cutover: the verb exists, the act is a person's on the day (the-cutover.md § the cutover). No removal of `internal/connect` yet — it goes with `migrate` in 0.9.0. No store requiring verified authors (decision 0019). No walk edges with a `From`: chronicle's walk names the label a node was reached by, not the node it came from. No semantic search without a provider on the install.
