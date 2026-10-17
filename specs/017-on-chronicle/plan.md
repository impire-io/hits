# Plan 017 — HITS on chronicle

## Layout

```
contract/state.go               paths, ItemState/ItemFromState and the vocabulary siblings, Diff, ItemOperation, VocabOperation
client/store.go                 TypeDeclarations, IndexDeclarations, Announcement, Init, Manifest
client/client.go                Client on chronicle's client; APIError and refusal; head (the exactness recipe), send, settle
client/items.go                 the item requests, GetItem, ListItems/ListAllItems, exec (the write loop), CreateItem and mint, the cross-instance checks
client/vocab.go                 projects, initiatives, releases: requests, loops, listings, the ship gate
client/search.go                SearchItems: the text index joined with state; an empty query is the listing
client/semantic.go              SemanticSearch over the meaning index
client/graph.go                 GraphNeighbors and GraphWalk over the rel index; node refs by path
internal/dial/dial.go           the connection seam: a chronicle context, loaded and dialled
internal/config/config.go       the selected initiative
internal/cli/cli.go             Run, Connector, the dispatch; store and migrate added; context, auth, ping, --actor removed
internal/cli/store.go           store init, store manifest
internal/cli/migrate.go         the cutover's verb and its verification
internal/cli/audit.go           ListAllItems; numbering-gap; unverified-write with --since-seq
internal/mcp/server.go          Connector on the chronicle context; no actor
internal/chronicletest/         the quick start in-process for every suite
client/client_test.go           the write loop, the counter, the vocabularies, search, graph, semantic
internal/cli/*_test.go          the sentences, the audit, the releases, on the new harness
internal/mcp/mcp_test.go        the tools, on the new harness
.golangci.yml                   the fleet rules gone
.goreleaser.yaml                install.json generated before the build, shipped in the archive
specs/017-on-chronicle/
removed: internal/node, internal/index, internal/fleet, cmd/hits-node, cmd/hits-index-*, internal/cli/{contexts,auth}.go
```

## Mechanics

- **A read is exact.** `head` reads the state bucket's value and folds the operations after it with `ccontract.FoldStep` under the resolved type record — the recipe of chronicle-hq design 03. It is what makes a write's guard the last operation's sequence (notes and marked operations move no state), and what makes a read right after a write see the write.
- **A write settles.** After a merge the client waits, bounded, for the bucket to carry the ack's sequence, so the listings — the status view, the ship gate — see what the same client just wrote. Never required for correctness.
- **Moved means read again.** A guard miss, and a precondition chronicle's pre-flight refused after the client's own check passed, both mean the state moved under the check; the loop reads again and the refusal, if it stands, carries the model's name (`already-claimed`, not `refused`).
- **A create's precondition is the guard's twin.** `create` carries `maxProperties: 0`; its refusal is reported as the instance existing, the same word the create-only guard uses.
- **The schemas require what a diff carries.** An operation's schema names the fields the difference always holds (`claim`: both claim fields; `steal`: `stolen-from`; `block`: the status and the interrupted one), never a field the fold may leave unchanged.

## Verification

`make check`: fmt, tidy, build, test (race on), lint. The client suite proves the loop and every refusal name; the CLI and MCP suites prove the sentences and tools on the quick start; the migration is proven by hits-hq research 005's replay and runs the same code.
