# Plan 018

1. Split `client`: types and the `Client` interface stay; the implementation moves to `client/chronicle`, with `Ping` on the announcement; the chronicle-only verbs dial the chronicle client by type.
2. `client/fleet`: the v0.7.0 client over the frozen subjects, writes carrying the construction-time actor, listings walking the IDs as the 0.7 audit did, `unreachable` when nothing answers.
3. `internal/connect.Resolve`; `internal/dial.Context(name, actor)` chooses by context and settles the actor; the CLI's `--actor`, `context`, `auth`, `ping`; `hits-mcp --actor`.
4. `internal/fleettest` builds the v0.7.0 fleet from its tag into the user cache (`HITS_FLEET_BIN` overrides) and runs it on `natstest`; `internal/embedtest` feeds both semantic indexes; `client/clienttest` is the surface suite both substrate tests run.
5. README, AGENTS.md, the lint fence widened to `client/**`.

Constitution check: the wire contract under test against real NATS and the real v0.7.0 binaries; no mock; `make check` green; signed commits.
