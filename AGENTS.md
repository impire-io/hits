# Agent guide for hits

Durable instructions for any coding agent working in this repository. The full
rules live in `../hits-hq/00-META/`; this file is the orientation and the
non-negotiables.

## Orientation (read in this order)

1. [`../hits-hq/00-META/mission.md`](../hits-hq/00-META/mission.md) — what HITS
   is and refuses to become: headless issue tracking, the API is the product,
   agents are the first-class operators, nothing is lost.
2. [`../hits-hq/00-META/how-we-build.md`](../hits-hq/00-META/how-we-build.md) —
   the binding engineering postures: minimal build, the history is the record
   and chronicle keeps it, HITS runs no service of its own, headless. When this repo and that
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
- **Tested against real NATS**: every suite boots chronicle's quick start
  in-process (`internal/chronicletest`); mocking the NATS client is
  forbidden.
- **Minimal build**: every addition names the present need it serves; "we might
  want it later" is not a need.
- **One surface**: everything callable goes through the `client` package's
  contract; no side doors, no HTTP-first parallel surface.
- **Connections go through `internal/dial`** — the one seam: a chronicle
  context, loaded with chronicle's `cli.LoadContext` and dialled with
  `Context.Dial`; the author of every write is the context's principal
  (`hits-hq/02-DESIGN/the-client.md`, decision 0021). Never a hand-rolled
  URL, never a dial outside the seam.
- **Nothing of HITS runs as a service.** The store, its state and its indexes
  are chronicle's; a client reads them exactly (the state, then the
  operations after it folded) and writes the difference the model's own
  fold produced, through chronicle's node under the guard.
- Commits are signed. `.claude/settings.local.json` is never committed.

## Layout

- `cmd/hits` — CLI binary; thin main over `internal/cli` (testable `Run` with
  an injectable connector). Its `store init` declares a HITS in a chronicle
  account; `migrate` (release 0.8.0 only) moves a fleet-era install in.
- `cmd/hits-mcp` — the MCP server binary, the agent action surface; thin main
  over `internal/mcp` (stdio MCP via the official Go SDK, one tool per client
  method). An adapter like the CLI, never a service.
- `contract` — the shared contract: op envelope and catalog, item and
  vocabulary models, invariants, the pure fold, and `state.go` — the mapping
  between the model and chronicle's state (paths, `ItemState`, `Diff`). It
  imports no other hits package (depguard-enforced).
- `client` — the public Go client package on chronicle's Go client
  (`hits-hq/02-DESIGN/the-client.md`): the store's declarations (`store.go`),
  the write loop, the reads and the three queries. Imports `contract` and
  chronicle; never `internal` (depguard-enforced).
- `internal/dial` — the one connection seam: a chronicle context, loaded and
  dialled as chronicle's CLI dials it. Never a hand-rolled URL.
- `internal/config` — the selected initiative, the client's one piece of
  configuration.
- `internal/connect` — the fleet-era hits contexts, kept for `migrate` alone;
  leaves with release 0.9.0.
- `internal/chronicletest` — chronicle's quick start in-process for every
  test suite.
- `internal/version` — the build-stamped version shared by both binaries.
