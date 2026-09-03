# Spec 006 — hits-mcp, the agent action surface

**Work ID:** `006-hits-mcp`
**Design:** `hits-hq` @ `e83738d` —
[`02-DESIGN/mcp-server.md`](../../../hits-hq/02-DESIGN/mcp-server.md);
settled by decision
[`0003`](../../../hits-hq/03-DECISIONS/0003-mcp-server.md).
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting
review and merge.

## What this delivers

The fifth binary: `cmd/hits-mcp`, an MCP stdio server exposing the client
API to agents as tools. It is a client of the fleet, never a peer — no
NATS endpoints, no subscriptions, no projection, no state. Every tool call
is one `client` package call; the MCP layer adds no vocabulary of its own.

## Out of scope (own decisions later)

- Streamable HTTP or any network transport — nothing is deployed.
- MCP resources and prompts — no present consumer.
- Folded convenience tools, batching, server-side workflow.
- Any auth story beyond the claimed startup actor (decision 0002's
  tightening path).

## Requirements

- **FR-01** A separate binary `cmd/hits-mcp`, its logic in `internal/mcp`,
  importing the `client` package (and `contract`) but never
  `internal/node` or `internal/index`; no service imports it. Both
  directions depguard-enforced — and the same adapter rule now also binds
  `internal/cli`, making spec 005's stated boundary mechanical.
  (decision 0003)
- **FR-02** Eighteen tools, one per client endpoint, exactly the design's
  table: the twelve item tools, `register_project`/`list_projects`,
  `search_items`, `semantic_search`, `graph_neighbors`/`graph_walk`.
  Nothing else is callable. The six query tools carry the MCP read-only
  annotation. `ping` is the startup health check, not a tool.
  (mcp-server § the tool surface)
- **FR-03** Tool input schemas restate the client request types field for
  field, wire names included (`located-in`, `discovered-while`), minus
  `actor`. Schemas stay loose — no enums — so invalid values reach the
  service and come back as named invariants, never as schema errors.
  Replies return unchanged: the reply value is the structured output and
  its JSON is the content. (mcp-server § the tool surface)
- **FR-04** The actor is server configuration: `--actor`, falling back to
  `HITS_ACTOR`; required, validated for form at boot
  (`contract.ValidActor`), stamped on every write. No per-call override.
  (mcp-server § actor, decision 0002)
- **FR-05** Fail-fast startup: resolve the NATS context (`--context`,
  `natscontext`), connect, ping the `hits` service — and only then serve
  MCP over stdio. A server that cannot reach the fleet exits non-zero
  instead of exposing tools that cannot work. One long-lived connection;
  stateless throughout. (mcp-server § shape)
- **FR-06** Service rejections pass through verbatim: an invariant failure
  becomes a tool result with `isError` and the machine-legible
  `code: message` text unchanged. (mcp-server § errors)

## Constitution check

Against `AGENTS.md` and `how-we-build.md`: one surface — every tool goes
through the `client` package, no side doors (FR-01/02); headless — the MCP
server is an adapter on the NATS surface, not a second front door (FR-01);
machine-legible errors preserved (FR-06); NATS contexts only (FR-05);
minimal build — stdio only, no resources, no folded tools, non-goals
stated. The one new dependency (`modelcontextprotocol/go-sdk`) is named by
decision 0003. No conflicts.

## Acceptance

- `make check` green; no skipped tests; race detector on.
- Wire tests through an in-memory MCP client session against embedded
  JetStream with the real services live — node for the item tools,
  search/graph for the queries, semantic against the fake
  OpenAI-compatible provider — never a mocked NATS client:
  - the full item lifecycle driven tool by tool, replies decoding into
    `contract.Item` (FR-02/03);
  - `tools/list` shows exactly the eighteen tools, read-only annotations
    on the six query tools (FR-02);
  - an invariant rejection surfaces its code verbatim in an `isError`
    result (FR-06);
  - a missing or malformed actor, and an unreachable fleet, refuse to
    serve (FR-04/05);
  - `search_items`, `semantic_search`, `graph_neighbors`, `graph_walk`
    find what the item tools created (FR-02).
