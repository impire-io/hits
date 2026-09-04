# Spec 009 — standalone hits contexts

**Work ID:** `006-standalone-contexts-rework`
**Design:** `hits-hq` @ `f62dc6a` —
[`02-DESIGN/idp-auth.md`](../../../hits-hq/02-DESIGN/idp-auth.md);
settled by decision
[`0011`](../../../hits-hq/03-DECISIONS/0011-standalone-hits-contexts.md),
which supersedes 0010's schema and resolution; rooted in hits-hq issue
[`006-standalone-contexts-rework`](../../../hits-hq/04-ISSUES/006-standalone-contexts-rework/00-report.md).
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting
review and merge.

## What this delivers

Reworks spec 008's context shape to decision 0011: a hits context is a
**hits-owned document** — hits fields at the top level (today the
optional `oauth` block), the NATS connection nested under a `nats` key in
the exact orbit natscontext `Settings` schema — and the **only** place
hits reads connection configuration from. Resolution never falls back to
the nats CLI's contexts or its selected-context marker; the nats CLI's
directory becomes an import source, nothing more. Carrying it: the
`hits context` verb family (`ls`, `add`, `import`, `edit`, `rm`,
`select`), the paved path the standalone namespace needs.

## Out of scope (own decisions later)

- Everything spec 008 delivered that 0011 leaves standing: device flow,
  token cache, serialized refresh, the auth verbs, the seam boundary.
- A hits-own selection marker — `select` writes the 005 config's
  `defaults.context` and nothing else (decision 0011).
- The orbit upstream proposal (an exported `Settings`-to-options API);
  this spec ships the temp-file shim it would replace.

## Requirements

- **FR-01** A hits context file is `$XDG_CONFIG_HOME/hits/context/
  <name>.json`: top-level `oauth` (optional; `issuer` and `client_id`
  required inside it, `scopes` defaulting to `openid offline_access`) and
  a `nats` object holding the connection in the exact natscontext
  `Settings` schema. Unknown top-level keys and unknown `nats` keys pass
  through untouched. A missing `nats` object means the default
  connection, exactly as an empty nats context file would.
  (idp-auth § hits contexts)
- **FR-02** Resolution is the explicit `--context` value, else the 005
  config's `defaults.context`, looked up **only** in hits' context
  directory. The nats CLI's contexts and its selected-context marker are
  never read at connect time. A configured name whose file is missing
  fails with a plainly-worded error naming `hits context add` and
  `hits context import`. No name at all connects to the default NATS URL
  — zero-config localhost keeps working without touching the nats CLI's
  selection. (idp-auth § the seam)
- **FR-03** The seam feeds natscontext through a shim: the `nats` subtree
  is written byte-for-byte to a `0600` file in a fresh `0700` temp
  directory, its absolute path handed to `natscontext.Connect`, and the
  directory removed once the connect returns — full loader reuse
  (homedir/env expansion, nsc lookup, SOCKS, `tls_first`) with no
  hits-side connection schema. An `oauth` context still gets
  `nats.TokenHandler` layered through the variadic options.
  (idp-auth § the seam; decision 0011)
- **FR-04** An `oauth` block alongside a `nats.token` value fails before
  connecting with a plainly-worded error naming the file; nats.go's
  `ErrTokenAlreadySet` is never surfaced. The check reads the subtree
  through natscontext's own exported `Settings` type — the schema stays
  upstream's. (idp-auth § hits contexts)
- **FR-05** `hits context ls` lists every context in hits' directory,
  marking the config default; `--json` emits the same as JSON.
  `hits context rm <name>` deletes the file. Both touch nothing outside
  hits' directory. (idp-auth § the verbs)
- **FR-06** `hits context add <name> [--url <url>] [--issuer <url>
  --client-id <id>]` scaffolds a context file — the `nats` object with
  the url, plus an `oauth` block when both oauth flags are given (one
  without the other is an error) — refuses an existing name, and opens
  `$EDITOR` on the result when set, else prints the path.
  `hits context edit <name>` opens `$EDITOR` on the file and requires
  both the file and `$EDITOR` to exist. (idp-auth § the verbs)
- **FR-07** `hits context import <nats-context> [<name>]` reads the named
  file from the nats CLI's context directory (read-only, the only verb
  that looks there), wraps its content byte-for-byte under `nats`, and
  writes it as a hits context — refusing an existing target, defaulting
  the new name to the nats name. (idp-auth § the verbs)
- **FR-08** `hits context select <name>` verifies the context exists and
  writes `defaults.context` in `$XDG_CONFIG_HOME/hits/config.json`,
  preserving every other field the file carries. No other selection state
  exists. (idp-auth § the verbs; decision 0011)
- **FR-09** The auth verbs keep their contract on the new shape: they
  require a hits context with an `oauth` block, and their errors name the
  fix (`hits context add`/`import` when the context is missing, "no
  oauth block" when it lacks one). (idp-auth § the verbs)
- **FR-10** Wire behavior stays proven against real NATS: the embedded
  server connects through a nested-shape context (plain and oauth), an
  imported nats context connects identically, and a name that exists only
  in the nats CLI's directory does **not** resolve. Existing token
  handler, refresh, and lock tests keep passing on the new shape. The
  CLI usage text gains the `context` family, and `AGENTS.md`'s connection
  non-negotiable is reworded to the standalone shape.
