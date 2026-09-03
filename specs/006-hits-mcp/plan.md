# Plan 006 — hits-mcp

How [spec.md](spec.md) lands. Design authority: `hits-hq` @ `e83738d`.

## Layout

```
cmd/hits-mcp/       thin main over internal/mcp
internal/mcp/
  server.go         Run (flags, actor, fail-fast dial + ping, stdio)
                    and NewServer (tool registration on a client)
  items.go          the twelve item tools + the two project tools
  query.go          the four query tools
```

The SDK is `github.com/modelcontextprotocol/go-sdk` v1 (decision 0003),
imported under an alias so our package keeps the `mcp` name. depguard
grows an adapter rule — `internal/mcp` and `internal/cli` never import
`internal/node` or `internal/index` — and the two service rules learn to
deny the adapter trees back.

## Mechanics

- **Generic tool registration.** Each tool is `AddTool[In, Out]` with a
  local input struct restating the client request minus `actor` (wire
  names in json tags; pointer fields on `edit_item` preserve the
  set-vs-absent distinction) and the client reply type as `Out` — the SDK
  derives both schemas, validates input, and packs the reply as structured
  output plus JSON content. No enums in schemas: the service's invariants
  are the validator, so rejections keep their names.
- **Errors ride the handler's error return.** `ToolHandlerFor` turns a
  returned error into an `isError` tool result carrying the error text —
  which for `*client.APIError` is already `code: message`. Nothing is
  rewritten.
- **One connection, dialed before serving.** `Run` parses flags, resolves
  the actor (`--actor`, then `HITS_ACTOR`) and validates it with
  `contract.ValidActor`, connects through the injectable Connector
  (`natscontext` in production), pings the `hits` service, then hands the
  connection-bound `client.Client` to `NewServer` and runs the stdio
  transport. Any failure before that point is a non-zero exit.
- **`NewServer` is transport-free** so tests connect the same server over
  `NewInMemoryTransports` while production runs `StdioTransport`.

## Tests

Wire tests in `internal/mcp`, the CLI suite's harness pattern: embedded
JetStream (`internal/natstest`), real node and index services (semantic
against the deterministic bag-of-words provider), and an in-process MCP
client session over the in-memory transport pair. Lifecycle coverage
drives one item through the tools end to end, decoding structured content
into `contract.Item` at each step; `tools/list` is asserted for the exact
tool set and the read-only annotations; startup failures (bad actor, no
fleet) are asserted through `Run` with a failing Connector — the guard
Connector fails the test if a rejected boot ever dials.
