# Spec 007 — `hits up`, the fleet as one process

**Work ID:** `007-hits-up`
**Design:** `hits-hq` @ `2900257` —
[`02-DESIGN/hits-up.md`](../../../hits-hq/02-DESIGN/hits-up.md);
settled by decision
[`0004`](../../../hits-hq/03-DECISIONS/0004-hits-up.md).
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting
review and merge.

## What this delivers

The getting-started shape: `hits up --context <name>` runs the whole
service fleet as one process inside the one `hits` binary, against
whatever NATS system the context points at. The services are untouched —
`up` composes the same `Start` entrypoints the standalone `cmd/` mains
call, each service on its own connection under its standalone `nats.Name`.
A packaging change, not a topology change.

## Out of scope (own decisions later)

- An embedded NATS server — `up` connects to a NATS system, never becomes
  one.
- A name-prefix knob or any multi-tenancy — one HITS per account, stated.
- Daemonizing, supervision, restarts — `up` is a foreground process.
- Shipping the standalone fleet binaries — they return when a production
  consumer asks.

## Requirements

- **FR-01** A bounded composition tree `internal/fleet` exposing `Start`:
  it runs `hits-node` and the three index services and stops them all on
  `Stop`, fail-fast in both directions — any service that cannot start
  stops the ones already running and returns the error. It imports the
  four service package roots and `contract`, nothing else; nothing imports
  it back. depguard-enforced both ways, with the existing adapter denials
  untouched. (hits-up § the composition)
- **FR-02** `cmd/hits/main.go` dispatches `up` before the client CLI's
  parser ever sees the arguments, so `internal/cli` stays a pure client.
  `up`'s flags follow the subcommand under its own flagset: `--context`,
  `--embedding-url`, `--embedding-model`. The CLI usage text lists `up`.
  (hits-up § the composition)
- **FR-03** Each service connects for itself through the same NATS context
  under its standalone `nats.Name` (`hits-node`, `hits-index-graph`, …),
  via an injectable per-service connector (`natscontext` in production,
  the embedded server in tests). (hits-up § connections)
- **FR-04** The semantic index is conditional in `up`: started only when
  `--embedding-url` and `--embedding-model` are both given (key from
  `HITS_EMBEDDING_API_KEY`); otherwise the other three boot and one clear
  line says semantic search is off and which flags turn it on. The
  standalone `hits-index-semantic` binary keeps requiring its flags.
  (hits-up § the semantic index is optional here)
- **FR-05** The ops-log names — the `hits-ops` stream, the `hits.ops.>`
  subject space, the projection bucket names — are declared once, in
  `contract`, and every service reads them from there. A service declaring
  its own copy is a defect. (hits-up § boundaries)
- **FR-06** depguard stops leaving `cmd/**` unconstrained: each main is
  pinned to its own tree — `cmd/hits` to `internal/cli` + `internal/fleet`,
  each service main to its service, `cmd/hits-mcp` to `internal/mcp`.
  (hits-up § boundaries)
- **FR-07** The release ships `hits` and `hits-mcp`: goreleaser gains the
  `hits-mcp` build and drops `hits-node` from builds and archives.
  (hits-up § what ships)

## Constitution check

Against `AGENTS.md` and `how-we-build.md`: one surface — `up` adds nothing
callable, it runs the fleet whose micro endpoints stay the product surface
(FR-01); headless — no new protocol, no UI; connections resolve through
NATS contexts only (FR-03); minimal build — the present need is onboarding,
named by decision 0004, and every non-goal is stated; the wire contract is
tested against real NATS via `internal/natstest`, never a mocked client.
No new dependencies. No conflicts.

## Acceptance

- `make check` green; no skipped tests; race detector on.
- Wire tests in `internal/fleet` against embedded JetStream
  (`internal/natstest`) with the real services — never a mocked NATS
  client:
  - with the embedding provider configured (the deterministic test
    provider), `Start` brings up all four services: an item created
    through the `client` package is found by search, graph, and semantic
    query (FR-01/03/04);
  - without embedding config, three services answer, semantic has no
    responder, and the notice line names the flags (FR-04);
  - a connector that fails for one service makes `Start` fail and leaves
    nothing running — no service answers afterwards (FR-01);
  - after `Stop`, no service answers and every connection is closed
    (FR-01);
  - `up` refuses half-configured embeddings (`--embedding-url` without
    `--embedding-model`, and the reverse) with a usage error (FR-04).
- `hits up` against a live context is exercised manually before merge —
  the spec's claim is the wire tests plus the gate.
