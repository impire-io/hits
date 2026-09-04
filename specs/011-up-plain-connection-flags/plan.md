# Plan 011 — plain connection settings for `hits up`

How [spec.md](spec.md) lands. Design authority: `hits-hq` @ `48b72da`.

## Layout

```
internal/connect/direct.go   Direct (the eight plain settings), Dial —
                             the seam's front door for binaries taking
                             both a context name and plain settings:
                             precedence, env fill (dormant without a
                             server URL), half-pair checks, and the dial
                             through an ephemeral Settings subtree
internal/connect/hitscontext.go  the temp-file loader shim generalizes to
                             subtreeFile([]byte); the context path calls
                             it unchanged
internal/fleet/fleet.go      the eight flags on `hits up`; the flag-level
                             context-vs-connection conflict check;
                             ContextConnector becomes DialConnector;
                             RunUp's connector factory takes (context,
                             Direct); upUsage names flags and variables
cmd/hits/main.go             passes fleet.DialConnector
internal/natstest/server.go  StartWithUserPass for the wire-truth test
README.md                    getting-started: plain flags and NATS_* env
                             first, contexts for saved configuration
                             (also retires the stale pre-0011 `nats
                             context save` instructions)
```

## Mechanics

- **One meaning for every spelling.** `Direct.settings()` maps the eight
  fields onto the exact `natscontext.Settings` schema; the subtree goes
  through the same 0600-temp-file shim a hits context's `nats` block
  takes. Auth selection (user over creds over nkey), homedir expansion,
  and TLS assembly stay upstream's — nothing re-implements connection
  semantics.
- **Provenance decides, once.** RunUp checks the only conflict it alone
  can see (both `--context` and connection *flags*); `Dial` owns the
  rest: env fill only when a server URL is in play, context-over-env,
  env-over-configured-default, half-pair refusals. Other binaries adopt
  the same semantics by calling `Dial` — nothing to copy.
- **The injected Connector stays the test seam.** RunUp's factory gains
  the `Direct` argument; fleet tests keep dialing the embedded server and
  gain a spy asserting the parsed flags reach the factory intact.

## Tests

- `internal/connect/direct_test.go` — the FR-04 precedence trio and the
  FR-06 wire truths against embedded servers (anonymous refused,
  user/password accepted, flagged and from env); FR-03/FR-05 refusals
  named before any dial; the FR-02 subtree keys asserted against the
  natscontext schema.
- `internal/fleet/fleet_test.go` — the context-vs-flags usage error fires
  before the connector dials; the spy proves flags land in `Direct`.
- `make check` green: fmt, tidy, build, race-detector tests, lint.
