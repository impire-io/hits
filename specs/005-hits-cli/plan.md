# Plan 005 — the hits CLI

How [spec.md](spec.md) lands. Design authority: `hits-hq` @ `53daf45`.

## Layout

```
cmd/hits/          unchanged: thin main over internal/cli
internal/cli/
  cli.go           Run: global flags, dispatch, usage; Connector (as today)
  items.go         the twelve item verbs
  projects.go      project register | list
  query.go         search, semantic, graph neighbors | walk
  print.go         human rendering + the --json path
```

No new dependencies. The dispatch stays stdlib `flag` — one `FlagSet` per
command, the pattern already in place; with thin per-command functions the
surface does not earn a CLI framework (minimal build).

## Mechanics

- **One shape per command.** Parse flags and positionals → build the
  `client` request → connect through the injected Connector → call → print.
  Connection happens after argument validation, so bad invocations never
  touch the wire (FR-02, FR-06).
- **Global flags before the command** (`--context`, `--actor`, `--json`),
  command flags after it, positionals last: `hits transition <id> --to
  resolved --fixed-by "commit:abc123"`. `flag`'s first-non-flag rule makes
  interleaving ambiguous; the usage text states the order.
- **Actor resolution**: `--actor` wins, `HITS_ACTOR` fills, empty fails
  with the two ways to set it. Read commands skip resolution.
- **`--fixed-by` parsing**: `kind:ref[ note]` — kind one of `pr`, `commit`,
  `action`; everything after the first space is the evidence note. Colons
  in the ref survive (split once).
- **Closing verbs are presets.** `resolve` and `wontfix` share
  `transition`'s one body with the target fixed and the `--to` flag
  dropped — same flags otherwise, same client call.
- **Human rendering** (`print.go`): an item as a labeled block — id, type,
  status, priority on the head line, then report, reporter/created, claim,
  block, located-in, closing refs, notes, links; empty fields omitted.
  Search/semantic as `score  id` rows (search adds the total); graph edges
  as `from -[type]-> to` rows; walk lists nodes then edges. `--json`
  bypasses rendering and re-encodes the reply indented.
- **Errors**: `*client.APIError` already prints `code: message`; `Run`
  returns errors up to main's `hits: <err>` + exit 1. No rewriting.

## Tests

Wire tests in `internal/cli`, through `cli.Run` with an injected Connector
against embedded JetStream (`internal/natstest`), real services on the
other side — the existing `startNode` helper grows into a harness that can
also start the search, graph, and semantic services (the semantic provider
faked with the same deterministic bag-of-words endpoint the client tests
use). Lifecycle coverage drives one item through create → claim → block →
unblock → transition → note → link → tombstone by invoking the actual
commands and asserting on their printed output; flag-parsing edge cases
(missing actor, bad `--fixed-by`, unknown command) assert on the error
before any connection is made — the Connector in those tests fails the test
if called.
