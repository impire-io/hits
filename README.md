# hits

HITS is a headless, agent-native issue tracking system, built on
[chronicle](https://github.com/impire-io/chronicle). There is no web UI and
no HTTP API: a HITS is one store in a chronicle account — four types,
three indexes, every change an operation with an author in the history —
and the two binaries here are clients of it. Agents operate it through an
MCP server, humans through a CLI. Nothing of HITS runs as a service. The
same two binaries still speak to a HITS of release 0.7.0, the one that ran
its own fleet: that fleet is frozen at its release and gets no fix, but
the client reaches it through a hits context, and the sentences are the
same on both.

The mission, designs, decisions, and process live in
[`hits-hq`](../hits-hq/); this repo carries the code. Agents working in this
repo: read [AGENTS.md](AGENTS.md) first.

## Getting started

**1. Get the binaries.** One brew install gets both `hits` (the CLI) and
`hits-mcp` (the MCP server for agents):

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

**2. Have a chronicle to speak to.** HITS connects through a chronicle
context, saved by chronicle's own CLI (`brew install impire-io/tap/chronicle`):

```sh
chronicle login                      # the hosted account at chronicle.impire.dev, or
chronicle context add prod --url nats://nats.acme.example:4222 --creds hits.creds
                                     # a credential on the NATS you already run, with chronicle's node on it
chronicle up                         # or a local quick start, for a try
```

The selected context is the one `hits` speaks through; `--context <name>`
names another. Every write's author is the context's principal — your
GitHub login on the hosted account, a service account's name for an agent
— and chronicle vouches for it; `--actor` is refused here.

**Or a 0.7 fleet.** A HITS that still runs hits-node and its index
services (release 0.7.0, frozen) is reached through a hits context, as it
always was:

```sh
hits context add team --url nats://nats.acme.example:4222   # or 'hits context import' from a nats context
hits --context team --actor you ping
```

A name resolves against chronicle's contexts first, then hits's own; with
no `--context`, chronicle's selection, else hits's default. A write on a
hits context needs an actor — `--actor`, `HITS_ACTOR`, or the hits config's
default — because the 0.7 node validates a claimed actor. `hits ping`
names what answers on either substrate.

**3. Declare the store**, once per account:

```sh
hits store init
```

It creates the store `hits` with full history, defines the four types with
their lifecycle rules, declares the search, meaning and relation indexes,
and announces HITS to the account's console. Running it again finishes
what was left; nothing is created twice.

**4. Work with it.**

```sh
hits initiative register acme "Acme"
hits initiative select acme
hits project register acme-web "The web app"
hits create --type bug --project acme-web "login loop keeps repeating"
hits search login
hits graph neighbors acme-1
hits claim acme-1
hits resolve acme-1 --fixed-by pr:acme/web#42
```

Run `hits` with no arguments for the full command list. Every item verb,
the three vocabularies, `search`, `semantic`, `graph` and `audit` read as
they always have.

**5. Hand it to your agents.** `hits-mcp` is a stdio MCP server exposing the
full client surface as tools, one tool per method. For example, in an MCP
client configuration:

```json
{
  "mcpServers": {
    "hits": {
      "command": "hits-mcp",
      "args": ["--context", "prod"]
    }
  }
}
```

One server serves one agent on one context — the context is who the agent
is. A host running several agents runs several servers. On a hits context
the server takes the actor at startup (`--actor`, or `HITS_ACTOR`), as 0.7
did; on a chronicle context it refuses one.

Agents that drive the CLI directly can learn it from the `hits-cli` skill,
published in [`impire-marketplace`](https://github.com/impire-io/impire-marketplace)
for any Agent-Skills-capable agent (`npx skills add impire-io/impire-marketplace`)
and as a Claude Code plugin (`/plugin marketplace add impire-io/impire-marketplace`,
then `/plugin install hits@impire`). The skill also covers installing the
binary itself.

**Semantic search** needs an embedding provider on the chronicle install —
the install's configuration, not HITS's; `hits semantic <text>` joins the
query surface where one is configured.

## The install manifest

A hosted chronicle console can enable a HITS for an account from the
install manifest the release carries, `install.json` — the same
declaration `hits store init` applies, as data (`hits store manifest`
prints it). It names the store, the types and the announcement, never a
credential.

## Moving an existing install

`hits migrate --from <hits context>`, run on a chronicle context, moves a
0.7 fleet's HITS in: it reads that install's `hits-ops` stream and writes
every operation into the store with its original id, actor and time, then
verifies every entity's state against the old install's own fold and
refuses the move on any difference. The verb stays as long as a 0.7 fleet
exists; a fleet that needs a fix moves, it is not fixed.

## What a HITS keeps

Items with a lifecycle — bug, task, improvement; open, diagnosing,
located, blocked, resolved, wontfix — their claims, blocks with memory,
typed links, trail of notes and closing refs; three registered
vocabularies: initiatives, the projects inside them, and the releases
items aim at. The model is `hits-hq`'s
[`02-DESIGN/item-model.md`](../hits-hq/02-DESIGN/item-model.md); where it
lives is [`02-DESIGN/the-store.md`](../hits-hq/02-DESIGN/the-store.md).

## License

[Sustainable Use License](LICENSE) — free for internal business,
non-commercial, and personal use. Contributions under the DCO
([CONTRIBUTING.md](CONTRIBUTING.md)).
