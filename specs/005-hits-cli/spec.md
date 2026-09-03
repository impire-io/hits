# Spec 005 — the hits CLI

**Work ID:** `005-hits-cli`
**Design:** `hits-hq` @ `53daf45` —
[`02-DESIGN/services.md`](../../../hits-hq/02-DESIGN/services.md) (the
surface the CLI fronts),
[`02-DESIGN/item-model.md`](../../../hits-hq/02-DESIGN/item-model.md) (the
data it renders),
[`00-META/how-we-build.md`](../../../hits-hq/00-META/how-we-build.md)
§ headless — "human ergonomics are the job of the views built on top".
**Status:** implemented on this branch ([plan.md](plan.md)) — awaiting
review and merge.

## What this delivers

The `hits` CLI grows from the walking skeleton (`ping`, `version`) into the
terminal view of the whole client surface: every endpoint the `client`
package exposes gets a command. The CLI invents no contract — it is a
client of the fleet like any other caller, and everything it can do goes
through the `client` package.

The command surface, item verbs top-level (they are the daily workflow),
queries and the project vocabulary grouped:

```
hits [--context <name>] [--actor <handle>] [--json] <command>

  create      open an item            claim      record intent to work
  get         one item's snapshot     release    hand a claim back
  edit        non-lifecycle changes   block      block, remembering status
  transition  move the status        unblock    restore interrupted status
  link        assert a typed edge     note       append a trail entry
  unlink      retract an edge         tombstone  void a filing mistake

  project register | list             the located-in vocabulary
  search                              full-text over reports and notes
  semantic                            nearest items to a text
  graph neighbors | walk              edges at a node, bounded expansion

  ping · version                      unchanged
```

## Out of scope (own specs later)

- Any new endpoint, subject, or payload — the CLI only fronts what the
  `client` package already declares.
- Interactive or TUI modes, watch/follow, shell completion.
- Identity beyond the actor handle — authority stays the caller's claim
  until identity derives from NATS authentication (decision 0002).

## Requirements

- **FR-01** Every `client` package endpoint has exactly one command:
  the twelve item verbs, `project register`/`project list`, `search`,
  `semantic`, `graph neighbors`/`graph walk`, plus the existing `ping` and
  `version`. The CLI calls only the `client` package — never raw subjects.
  (AGENTS.md: one surface)
- **FR-02** Write commands name their actor: `--actor`, falling back to
  the `HITS_ACTOR` environment variable; absent both, the command fails
  before touching the wire with an error saying how to supply it. Read
  commands need no actor. (services § actors, decision 0002)
- **FR-03** Human-readable output by default; `--json` re-encodes the
  service reply as indented JSON for scripting. Service rejections pass
  through with their machine-legible code (`code: message` on stderr, exit
  non-zero) — never rewritten. (how-we-build § agents are first-class)
- **FR-04** Connections resolve through NATS contexts
  (`natscontext`, `--context` naming one, default the selected context) via
  the injectable Connector, so every command is testable against an
  embedded server. (AGENTS.md: connections)
- **FR-05** `transition` carries the closing refs: repeatable `--fixed-by`
  (`pr:`, `commit:`, or `action:` followed by the ref, an optional note
  after the first space) and `--amended-design`; `create`/`edit`/
  `transition` carry `--project` (repeatable, located-in), `edit` accepts
  `--lands` as a JSON array — the one deeply structured field stays JSON.
  (item-model § properties)
- **FR-06** `hits <command> -h` and bare `hits` print usage; an unknown
  command or malformed flags print usage and fail.

## Constitution check

Against `AGENTS.md` and `how-we-build.md`: one surface — the CLI is a
client of the fleet, no side doors (FR-01); headless — the CLI is a view,
the API remains the product (scope); machine-legible errors preserved
(FR-03); NATS contexts only (FR-04); minimal build — no new endpoints, no
TUI, no completion, non-goals stated. No conflicts.

## Acceptance

- `make check` green; no skipped tests; race detector on.
- Wire tests through `cli.Run` against embedded JetStream with the real
  services live — node for the item verbs and projects, search/graph for
  the queries, semantic against a fake OpenAI-compatible provider — never
  a mocked NATS client:
  - the full item lifecycle driven command by command, each printing the
    changed snapshot (FR-01);
  - a write command without an actor fails before connecting; with
    `HITS_ACTOR` set it succeeds (FR-02);
  - an invariant rejection surfaces its code verbatim (FR-03);
  - `--json` output round-trips through `json.Unmarshal` (FR-03);
  - `search`, `semantic`, `graph neighbors`, `graph walk` find what the
    item verbs created (FR-01).
