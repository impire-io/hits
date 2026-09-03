# Plan 007 — hits up

How [spec.md](spec.md) lands. Design authority: `hits-hq` @ `2900257`.

## Layout

```
cmd/hits/main.go    dispatches "up" before cli.Run; everything else unchanged
internal/fleet/
  fleet.go          Start/Stop (composition, fail-fast, reverse-order stop)
                    and RunUp (the up flagset, notice lines, signal wait)
  fleet_test.go     wire tests against embedded JetStream
contract/names.go   the ops-log names, declared once
```

## Mechanics

- **`Start(ctx, connect, cfg)` composes; order matters.** `hits-node`
  starts first — it creates or updates the `hits-ops` stream and the
  buckets — then the indexers, which refuse to start without the stream.
  Each service gets its own connection from the injectable
  `Connector func(natsName string) (*nats.Conn, error)`, called with the
  service's standalone `nats.Name`; production's connector resolves the
  same NATS context per call (`natscontext`), tests dial the embedded
  server. Any failure stops what already started (reverse order) and
  closes its connections before returning.
- **`Stop` is the reverse of `Start`**: indexers first, node last, then
  every connection closed. Idempotent enough to be deferred.
- **Semantic is a config presence check.** `cfg.Semantic` (the service's
  own `semantic.Config`) empty means: not started, and `RunUp` prints one
  line naming `--embedding-url`/`--embedding-model`. Half-configured
  embeddings are a usage error before anything connects.
- **`RunUp` owns the subcommand**: its own `flag.FlagSet` (`--context`,
  `--embedding-url`, `--embedding-model`), key from
  `HITS_EMBEDDING_API_KEY`, per-service startup lines with the version,
  then blocks on ctx until signalled. `cmd/hits/main.go` routes
  `args[0] == "up"` to it and everything else to `cli.Run`, so the client
  tree never sees service code. The `internal/cli` usage text gains the
  `up` line (text only, no import).
- **`contract/names.go`** exports the stream name, the ops subject space
  and per-kind prefixes, and the projection bucket names; `internal/node`
  and the three indexers drop their private copies.

## depguard

- `fleet-composes-the-services`: `internal/fleet` may import the four
  service roots and `contract`; denied `client`, `internal/cli`,
  `internal/mcp`.
- The service and adapter rules each gain a deny of `internal/fleet` —
  composition is a one-way street.
- New per-main rules pin `cmd/**`: `cmd/hits` to `internal/cli` +
  `internal/fleet` (never a service root directly), `cmd/hits-mcp` to
  `internal/mcp`, each service main to its own service tree only.

## goreleaser

The `hits-mcp` build joins with the same flags; the `hits-node` build and
its archive slot leave. Archives carry `hits` and `hits-mcp`.

## Tests

Wire tests in `internal/fleet`, the existing harness pattern: embedded
JetStream (`internal/natstest`), a local deterministic OpenAI-compatible
provider (the `fakeProvider` shape the CLI and MCP suites use), and the
`client` package as the probe. Covered: all four services answering after
a configured `Start` (create → search/graph/semantic find it); the
three-service boot without embeddings, asserting the notice line and no
semantic responder; fail-fast on a connector that fails for a later
service, asserting nothing is left answering; clean `Stop`; the
half-configured-embeddings usage error through `RunUp`.
