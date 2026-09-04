# hits

HITS is a headless, agent-native issue tracking system. There is no web UI
and no HTTP API: the platform is a set of NATS micro services, agents
operate it through an MCP server, and humans through a CLI. It runs against
the NATS system you already have — [Synadia Cloud](https://www.synadia.com/cloud)
or your own JetStream-enabled server — and owns no infrastructure of its own.

The mission, designs, decisions, and process live in
[`hits-hq`](../hits-hq/); this repo carries the code. Agents working in this
repo: read [AGENTS.md](AGENTS.md) first.

## Getting started

**1. Get the binaries.** One brew install gets both `hits` (the CLI, which
can also run the whole platform) and `hits-mcp` (the MCP server for agents):

```sh
brew install impire-io/tap/hits
```

Or download the archive for your platform from the
[releases page](https://github.com/impire-io/hits/releases), or build from
source:

```sh
go install github.com/impire-io/hits/cmd/hits@latest
go install github.com/impire-io/hits/cmd/hits-mcp@latest
```

**2. Point it at a NATS system.** Connections resolve through
[NATS contexts](https://docs.nats.io/using-nats/nats-tools/nats_cli#nats-contexts).
The system needs JetStream enabled; one account (or JetStream domain) hosts
one HITS.

```sh
# your own server
nats context save hits --server nats://localhost:4222

# Synadia Cloud: point at NGS with the creds file of a JetStream-enabled account
nats context save hits --server tls://connect.ngs.global --creds ~/ngs/hits.creds
```

**3. Run the platform.**

```sh
hits up --context hits
```

That single process runs the whole service fleet — the item store plus the
graph and full-text indexes. Nothing to provision first: the ops-log stream
and its projections are created on boot. The fleet shares one NATS
connection, and every resource declares a byte budget (1 GiB for the ops
log by default — some accounts, Synadia Cloud included, require one);
change it with `--max-bytes 2G`. Runs in the foreground until interrupted.

**4. Work with it.** Every write names an actor — set `HITS_ACTOR` (or pass
`--actor`) to your handle:

```sh
export HITS_ACTOR=daan

hits --context hits project register hits "The hits repo"
hits --context hits create --type bug --project hits "login loop keeps repeating"
hits --context hits search login
hits --context hits graph neighbors 1
hits --context hits claim 1
hits --context hits resolve 1 --fixed-by pr:42
```

Run `hits` with no arguments for the full command list.

**5. Hand it to your agents.** `hits-mcp` is a stdio MCP server exposing the
full client surface as tools, one tool per endpoint. For example, in an MCP
client configuration:

```json
{
  "mcpServers": {
    "hits": {
      "command": "hits-mcp",
      "args": ["--context", "hits"],
      "env": { "HITS_ACTOR": "my-agent" }
    }
  }
}
```

One server serves one agent under one actor; a host running several agents
runs several servers.

Agents that drive the CLI directly can learn it from the `hits-cli` skill,
published in [`impire-marketplace`](https://github.com/impire-io/impire-marketplace)
for any Agent-Skills-capable agent (`npx skills add impire-io/impire-marketplace`)
and as a Claude Code plugin (`/plugin marketplace add impire-io/impire-marketplace`,
then `/plugin install hits@impire`). The skill also covers installing the
binary itself.

**Optional: semantic search.** The semantic index needs an
OpenAI-API-compatible embedding provider and is off unless configured:

```sh
export HITS_EMBEDDING_API_KEY=...
hits up --context hits --embedding-url https://api.openai.com/v1 --embedding-model text-embedding-3-small
```

Everything else works without it; `hits semantic <text>` joins the query
surface when it is on.

## Layout

| Path | What it is |
|---|---|
| `cmd/hits` | The terminal client, and — through `hits up` — the whole fleet in one process (thin main; logic in `internal/cli` and `internal/fleet`). |
| `cmd/hits-mcp` | The MCP server: the agent action surface (thin main; logic in `internal/mcp`). |
| `cmd/hits-node` | The item-store micro service, standalone (thin main; logic in `internal/node`). |
| `cmd/hits-index-graph` | The graph index service, standalone (thin main; logic in `internal/index/graph`). |
| `cmd/hits-index-search` | The full-text index service, standalone (thin main; logic in `internal/index/search`). |
| `cmd/hits-index-semantic` | The semantic index service, standalone (thin main; logic in `internal/index/semantic`). |
| `contract` | The shared platform contract: op envelope, item model, invariants, fold, resource names. |
| `client` | The public Go client package — the one way callers talk to the services. |

`hits up` is the getting-started and small-team shape; the standalone
service binaries compose the same fleet as separate processes when you want
them scheduled individually. Build them with `make build`.

## Build & run from source

```sh
make check   # fmt + tidy + build + test + lint — the quality gate
make build   # all binaries land in bin/
```

## Releases

Pushing a `v*` tag builds and publishes a GitHub release via goreleaser
([release workflow](.github/workflows/release.yml)): one archive per
platform carrying `hits` and `hits-mcp`, with the tag's version stamped into
every binary. CI runs the same gate as `make check` on every push and pull
request.

## License

[Sustainable Use License](LICENSE) — free for internal business,
non-commercial, and personal use.
