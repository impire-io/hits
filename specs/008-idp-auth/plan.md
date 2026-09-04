# Plan 008 — IDP auth & hits contexts

How [spec.md](spec.md) lands. Design authority: `hits-hq` @ `c142363`.

## Layout

```
internal/connect/
  connect.go       Connect(contextName, natsName): resolve → lookup → natscontext,
                   layering nats.TokenHandler for oauth contexts
  resolve.go       name resolution (flag > config default > nats selected marker)
                   and the two-directory lookup with the collision error
  hitscontext.go   hits context file: natscontext schema + oauth block, the
                   oauth+token conflict check
  config.go        $XDG_CONFIG_HOME/hits/config.json — defaults {context, actor}
  oauth.go         OIDC discovery, device grant, refresh (golang.org/x/oauth2)
  cache.go         XDG state token cache: atomic rename, flock, freshness re-check
  *_test.go        embedded NATS (token auth) + httptest IDP
internal/cli/     auth verb family (login/status/logout); ContextConnector → seam;
                   actor default from config
internal/fleet/   ContextConnector → seam
internal/mcp/     server connect → seam
cmd/hits-node/, cmd/hits-index-*/  mains connect → seam
AGENTS.md          connection non-negotiable reworded to name the seam
```

## Mechanics

- **The seam wraps, never replaces, natscontext.** A hits context is
  handed to `natscontext.Connect` by absolute file path (its loader
  accepts one, and unknown JSON fields pass through harmlessly); a nats
  context goes by name, exactly today's call. All url/creds/TLS handling
  stays upstream — the seam's own surface is resolution, the oauth peek,
  and the token handler.
- **Resolution before connection.** natscontext resolves the selected
  context only while connecting, so the seam reads the nats CLI's
  selected-context marker itself when no name is given and no config
  default exists. Lookup order and the both-directories error are decided
  before any dial.
- **`oauth.go` uses `golang.org/x/oauth2`** — `DeviceAuth` for the grant,
  the token endpoint for refresh — with persistence ours, not a
  `TokenSource` cache: every refresh goes through `cache.go`'s
  lock-recheck-spend sequence, because rotating refresh tokens make a
  concurrent spend a grant-revoking hazard (design § tokens and refresh).
- **`cache.go` owns the file discipline**: `0700` directory, `0600` file,
  write-to-temp + rename, `flock` on a sibling lock file for the refresh
  critical section. Reads are lock-free.
- **The handler degrades in the designed order**: fresh cache → return;
  refreshable → refresh under lock, rewrite, return; refresh fails →
  return stale (the server rejects, nats.go backs off, the next attempt
  re-invokes); no cache at connect time → fail fast before dialing with
  the login-naming error.
- **Verbs are thin.** `login` = resolve, require oauth, run the grant,
  write cache; `status` = resolve + read cache; `logout` = delete cache.
  All three live in `internal/cli` beside the existing verbs, using the
  same output conventions.
- **Config is two fields.** `config.go` reads `defaults.context` and
  `defaults.actor`; a missing file is not an error. Actor precedence
  becomes flag > `$HITS_ACTOR` > default — the two existing sources keep
  their order.

## depguard

- `connect-is-shared-bootstrap`: `internal/connect` may import
  `natscontext`, `nats.go`, `oauth2`, and stdlib; denied `client`,
  `internal/cli`, `internal/mcp`, `internal/fleet`, and every service
  tree.
- `cmd/**`, `internal/cli`, `internal/fleet`, `internal/mcp` gain
  `internal/connect` in their allow lists; the service trees do not
  import it (their mains do the connecting, as today).

## Tests

- **Handler wire test**: embedded `nats-server` with token authorization;
  connect through the seam with an oauth hits context whose fake IDP
  serves the expected token — then expire it, force a reconnect, and
  assert the refreshed token connects. No NATS client mocking.
- **Rotation/lock test**: two concurrent refreshers against the fake IDP
  with rotation and reuse-detection semantics; exactly one token spend,
  both readers end on the new pair.
- **Resolution table test**: flag/default/selected × hits-dir/nats-dir/
  both/neither, including the collision error and the oauth+token
  conflict.
- **Verb tests**: golden output for login (device flow against the fake
  IDP), status, logout; the no-cache fail-fast message.
- **Parity test**: the same context file (creds/token/TLS fields) placed
  as a hits context and as a nats context yields identical connections.
