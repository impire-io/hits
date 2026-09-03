# Agent guide for hits

Durable instructions for any coding agent working in this repository. The full
rules live in `../hits-hq/00-META/`; this file is the orientation and the
non-negotiables.

## Orientation (read in this order)

1. [`../hits-hq/00-META/mission.md`](../hits-hq/00-META/mission.md) — what HITS
   is and refuses to become: headless issue tracking, the API is the product,
   agents are the first-class operators, nothing is lost.
2. [`../hits-hq/00-META/how-we-build.md`](../hits-hq/00-META/how-we-build.md) —
   the binding engineering postures: minimal build, ops-log as source of truth,
   every component a NATS micro service, headless. When this repo and that
   document disagree, that document wins.
3. [`../hits-hq/00-META/repos.md`](../hits-hq/00-META/repos.md) — the repo map
   and what this repo owns.
4. [`../hits-hq/02-DESIGN/`](../hits-hq/02-DESIGN/) — the designs this code
   implements. Capabilities land here through the build handoff
   ([playbook 04](../hits-hq/00-META/process/04-build-handoff.md)), not by
   invention in this repo.

## Non-negotiables

- **Quality gate before "done"**: `make check` (fmt + tidy + build + test +
  lint) — all green, no skipped tests, race detector on.
- **The wire contract is tested against real NATS** (`internal/natstest` runs
  an embedded server); mocking the NATS client is forbidden.
- **Minimal build**: every addition names the present need it serves; "we might
  want it later" is not a need.
- **One surface**: everything callable goes through the `client` package's
  contract; no side doors, no HTTP-first parallel surface.
- **Connections go through NATS contexts**
  (`github.com/synadia-io/orbit.go/natscontext`), never hand-rolled URLs.
- Commits are signed. `.claude/settings.local.json` is never committed.

## Layout

- `cmd/hits` — CLI binary; thin main over `internal/cli` (testable `Run` with
  an injectable connector).
- `cmd/hits-mcp` — the MCP server binary, the agent action surface; thin main
  over `internal/mcp` (stdio MCP via the official Go SDK, one tool per client
  endpoint, actor fixed at startup). An adapter like the CLI, never a fleet
  peer — the adapter boundary is depguard-enforced both ways.
- `cmd/hits-node` — service binary; thin main over `internal/node`.
- `cmd/hits-index-graph` — the graph index service; thin main over
  `internal/index/graph` (adjacency projection with derived project/actor
  edges, serving `hits.graph.*`).
- `cmd/hits-index-search` — the full-text index service; thin main over
  `internal/index/search` (a Bleve projection of the ops-log answering
  `hits.search.query`). Index services never import `internal/node`, nor it
  them (depguard-enforced).
- `cmd/hits-index-semantic` — the semantic index service; thin main over
  `internal/index/semantic` (chromem-go embedding projection with a
  configurable OpenAI-compatible provider, serving `hits.semantic.query`).
- `contract` — the shared platform contract: op envelope and catalog, item
  and project models, invariants, and the pure fold. Every service imports
  it; it imports no other hits package (depguard-enforced).
- `client` — the public Go client package. It also declares the wire contract
  (subjects, payload types); `internal/node` implements it. Imports only
  `contract` (depguard-enforced).
- `internal/version` — the build-stamped version shared by both binaries.
- `internal/natstest` — test-only embedded NATS server.
