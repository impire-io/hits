# Spec 008 — IDP auth & hits contexts

**Work ID:** `idp-auth`
**Design:** `hits-hq` @ `c142363` —
[`02-DESIGN/idp-auth.md`](../../../hits-hq/02-DESIGN/idp-auth.md);
settled by decisions
[`0008`](../../../hits-hq/03-DECISIONS/0008-client-idp-auth.md),
[`0009`](../../../hits-hq/03-DECISIONS/0009-connection-profiles.md),
[`0010`](../../../hits-hq/03-DECISIONS/0010-hits-contexts.md).
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting
review and merge.

## What this delivers

The client-side token exchange: `hits` authenticates against an identity
provider (device flow), holds the tokens, and feeds the current access
token into the NATS connection on every connect and reconnect. Carrying
it: **hits contexts** — hits-owned context files in the exact nats context
schema plus an `oauth` block, so connections configured outside the nats
CLI (OAuth, but equally creds, static tokens, TLS) live in one file hits
owns. The dividing line is fixed: HITS owns the exchange; the deployment's
auth callout owns validation. Also resolves hits-hq issue
`005-cli-config-file`: the client config file ships here, holding the
defaults (context, actor) this design needs.

## Out of scope (own decisions later)

- PKCE, client credentials — device flow only (decision 0008).
- OS keychain storage — file cache with tight modes (decision 0008).
- Verified actor identity — actor stays self-declared (decision 0002).
- A `hits context` verb family or hits-own selected-context marker — the
  config default covers selection (decision 0010).
- Server-side anything: no callout, no validation, no JWKS.

## Requirements

- **FR-01** A bounded `internal/connect` seam every binary connects
  through — `internal/cli`, `internal/fleet`, `internal/mcp`, and the four
  standalone service mains; no production caller dials `natscontext`
  directly anymore. It resolves the context name (explicit value, else the
  config default, else the nats CLI's selected-context marker), looks it
  up **hits context directory first, then the nats CLI's contexts**, and
  errors plainly when a name exists in both. (idp-auth § the seam)
- **FR-02** A hits context is `$XDG_CONFIG_HOME/hits/context/<name>.json`
  in the exact natscontext `Settings` schema plus the optional `oauth`
  block (`issuer`, `client_id` required; `scopes` defaulting to
  `openid offline_access`). Both lookup paths connect through
  `natscontext` — a hits context by absolute file path, a nats context by
  name — so creds, user/password, nkey, static token, and TLS fields work
  identically in either directory. (idp-auth § hits contexts)
- **FR-03** An `oauth` context whose file also sets `token` fails before
  connecting with a plainly-worded error naming the file; nats.go's
  `ErrTokenAlreadySet` is never surfaced. (idp-auth § hits contexts)
- **FR-04** The client config file `$XDG_CONFIG_HOME/hits/config.json`
  with `defaults`: `context` (used by FR-01 resolution) and `actor`
  (precedence: `--actor` flag, then `$HITS_ACTOR`, then the default).
  Absent file, absent fields: today's behavior exactly. This closes
  hits-hq `04-ISSUES/005-cli-config-file`. (idp-auth § hits contexts)
- **FR-05** `hits auth login [--context <name>]` runs the RFC 8628 device
  authorization grant against the context's IDP: endpoints from OIDC
  discovery, verification URL and user code printed, token endpoint
  polled at the server's interval (honoring `slow_down`), cache written on
  success. It requires the name to resolve to a hits context with an
  `oauth` block; anything else is a plainly-worded error. (idp-auth § the
  verbs)
- **FR-06** `hits auth status` prints the context in effect and which
  directory it came from, the token subject, and both expiries — plainly
  saying so when no cache exists. `hits auth logout` deletes the cache and
  states that IDP-side revocation is not attempted. (idp-auth § the verbs)
- **FR-07** The token cache is
  `$XDG_STATE_HOME/hits/tokens/<context>.json`, directory `0700`, file
  `0600`, written by atomic rename: access token, refresh token,
  access-token expiry. (idp-auth § tokens and refresh)
- **FR-08** OAuth contexts connect with `nats.TokenHandler`: on every
  connect and reconnect it returns the cached access token, first
  refreshing through the refresh token when expired or within 60 seconds
  of expiry. Refresh is serialized across processes — file lock on the
  cache entry, freshness re-check under the lock, only then is the refresh
  token spent. On refresh failure the handler returns the cached token
  (nats.go's reconnect loop retries and re-invokes it); an empty cache
  fails fast before connecting:
  `no token for context "<name>": run 'hits auth login --context <name>'`.
  No HITS-side session timers. (idp-auth § tokens and refresh)
- **FR-09** Wire behavior is tested against real NATS per the repo
  constitution: an embedded server configured for token auth proves the
  handler path (fresh token accepted, stale token refreshed then
  accepted); the IDP is an `httptest` fake speaking discovery, device
  authorization, and token endpoints — refresh rotation included, proving
  the lock prevents a spent-token reuse. Mocking the NATS client stays
  forbidden.
- **FR-10** Boundaries hold: depguard pins `internal/connect` as a shared
  tree importable by `cmd/**`, `internal/cli`, `internal/fleet`, and
  `internal/mcp`; it imports no service internals and no service imports
  it back. The CLI usage text gains the `auth` verb family, and
  `AGENTS.md`'s connection non-negotiable is reworded to name the seam:
  connections go through `internal/connect`, which resolves hits contexts
  or nats CLI contexts via `natscontext` — hand-rolled URLs stay
  forbidden.
