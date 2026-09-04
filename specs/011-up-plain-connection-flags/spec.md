# Spec 011 — plain connection settings for `hits up`

**Work ID:** `009-up-plain-nats-connection-flags`
**Design:** `hits-hq` @ `48b72da` —
[`02-DESIGN/hits-up.md`](../../../hits-hq/02-DESIGN/hits-up.md) § plain
connection settings, with the seam's ephemeral-context path noted in
[`02-DESIGN/idp-auth.md`](../../../hits-hq/02-DESIGN/idp-auth.md) § the
seam; rooted in hits-hq issue
[`009-up-plain-nats-connection-flags`](../../../hits-hq/04-ISSUES/009-up-plain-nats-connection-flags/00-report.md).
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting
review and merge.

## What this delivers

`hits up` takes the NATS connection directly — flags named as the nats
CLI names them, each falling back to the nats CLI's environment variable
— so CI jobs and containers point at a server without minting a context
first. An environment already set up for the `nats` CLI drives `hits up`
unchanged.

| flag | environment |
|---|---|
| `--server <urls>` (comma-separated) | `NATS_URL` |
| `--creds <file>` | `NATS_CREDS` |
| `--user` / `--password` | `NATS_USER` / `NATS_PASSWORD` |
| `--nkey <file>` | `NATS_NKEY` |
| `--tlscert` / `--tlskey` | `NATS_CERT` / `NATS_KEY` |
| `--tlsca <file>` | `NATS_CA` |

## Out of scope

- The client commands and `hits-mcp` — `up` is the stated need (the
  issue); the seam's `Dial` is ready for them when a need names itself.
- A `--token` flag — the nats CLI has none either; token deployments use
  a context (or its oauth block).
- A `NATS_CONTEXT` variable — hits has its own default-context
  configuration (`defaults.context`, issue 005).

## Requirements

- **FR-01** `hits up` accepts `--server`, `--creds`, `--user`,
  `--password`, `--nkey`, `--tlscert`, `--tlskey`, `--tlsca`; each falls
  back to its nats CLI environment variable. Precedence is flag →
  environment → configuration, per setting. (hits-up.md § plain
  connection settings)
- **FR-02** The settings connect through `internal/connect` as the
  `nats` subtree of an ephemeral context — the same natscontext loader a
  saved context takes, never written to the context directory — so plain
  settings and context files cannot drift apart in meaning. (idp-auth.md
  § the seam)
- **FR-03** `--context` alongside any connection flag is a hard,
  plainly-worded error before anything dials — a context carries its own
  connection.
- **FR-04** A flagged `--context` outranks `$NATS_URL`; `$NATS_URL`
  outranks the configured default context; the environment's auth
  variables activate only when a server URL is in play, so an exported
  `$NATS_CREDS` never bleeds into a context connection.
- **FR-05** The half-pairs natscontext ignores silently are hard errors:
  a password without a user, one half of a TLS cert/key pair. Plain auth
  or TLS settings without a server URL anywhere name the fix
  (`--server` / `$NATS_URL`).
- **FR-06** Tests prove the wire truth against real NATS: `$NATS_URL`
  alone connects, a flagged server outranks a dead `$NATS_URL`, a flagged
  context outranks a dead `$NATS_URL`, and user/password reach a server
  that requires them — flagged and from the environment. No NATS client
  mocks (constitution).
