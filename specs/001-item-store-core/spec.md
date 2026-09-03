# Spec 001 — Item store core

**Work ID:** `0001-item-store-architecture`
**Design:** `hits-hq` @ `826c108` —
[`02-DESIGN/item-model.md`](../../../hits-hq/02-DESIGN/item-model.md),
[`02-DESIGN/ops-log.md`](../../../hits-hq/02-DESIGN/ops-log.md),
[`02-DESIGN/services.md`](../../../hits-hq/02-DESIGN/services.md),
settled by decisions
[`0001`](../../../hits-hq/03-DECISIONS/0001-item-store-architecture.md) and
[`0002`](../../../hits-hq/03-DECISIONS/0002-projects-and-actors.md).
**Status:** draft — awaiting review before plan and implementation.

## What this delivers

The item store core, end to end through NATS: the shared contract package
(op envelope, item model, invariants), the ops-log write path with per-entity
ordering, the state projections, and the `hits` micro service served by
`hits-node`. After this feature, a caller can register projects and create,
read, claim, block, link, note, transition, and tombstone items entirely
through `hits.api.*`.

## Out of scope (own specs later)

- The three index services (`hits-index-graph`, `hits-index-search`,
  `hits-index-semantic`) — this spec produces the log they consume, not them.
- CLI verbs beyond the existing `ping`.
- The MCP server; auth-derived actor verification; external index stores.

## Requirements

Numbers cite the governing design section.

### Contract package

- **FR-01** One shared package declares the op envelope (`id`, `op`,
  `entity`, `actor`, `at`, `v`, `payload`), the op catalog, the item model
  (types, statuses, properties), and the invariants — importable by every
  service and by `client`, importing no service. (ops-log § envelope;
  services § code layout)
- **FR-02** No service package imports another service's packages; the
  boundary is lint-enforced in CI. (services § code layout)

### Ops-log write path

- **FR-10** All writes append to stream `hits-ops` (`hits.ops.>`,
  file-backed, unlimited retention); item ops on `hits.ops.item.<id>`,
  project ops on `hits.ops.project.<slug>`; op type in the envelope, never
  the subject. (ops-log § stream and subjects)
- **FR-11** Every append is read-validate-append under
  `Nats-Expected-Last-Subject-Sequence`; a CAS conflict re-reads and
  retries. Publishes are synchronous; the envelope `id` is the
  `Nats-Msg-Id`. (ops-log § ordering)
- **FR-12** Item IDs are dense decimal strings minted from a CAS-guarded
  counter in `hits-meta`; project slugs are caller-chosen and made unique by
  expected-sequence-zero publish. (ops-log § identifiers)

### Invariants

- **FR-20** `hits-node` rejects, with a machine-legible error naming the
  invariant, any command violating the item-model invariants: lifecycle
  transitions per type, no exit from terminal status, `blocked-by` only
  while blocked, claim fields paired, `located` requires `located-in`,
  tasks require `located-in` at creation, `located-in` names registered
  projects only, closing transitions carry the close date, tombstoned items
  accept no further ops, every command carries a well-formed `actor`.
  (item-model § invariants; services § hits-node)
- **FR-21** Blocking records the interrupted status; unblocking restores
  exactly it and clears `blocked-by`. (item-model § lifecycle)

### Projections

- **FR-30** `hits-node` folds ops into `hits-items` (snapshot +
  last-applied stream sequence, per-key history enabled) and registrations
  into `hits-projects`; folding is idempotent (ops at or below the recorded
  sequence are skipped) and snapshot writes CAS on KV revision.
  (ops-log § the state projection, § ordering)
- **FR-31** Deleting a projection bucket and replaying from sequence 1
  reproduces it exactly. (posture: ops-log is the source of truth)

### Service surface

- **FR-40** `hits-node` exposes micro service `hits` with endpoints
  `create`, `get`, `edit`, `transition`, `claim`, `release`, `block`,
  `unblock`, `link`, `unlink`, `note`, `tombstone`, `project.register`,
  `project.list` under `hits.api.`, answering the standard micro discovery
  surface. (services § hits-node)
- **FR-41** The `client` package declares the wire contract for every
  endpoint and is the one way callers talk to the service. (AGENTS.md;
  services § non-goals)

## Constitution check

This repo has no constitution file yet; the binding text is `AGENTS.md`'s
non-negotiables plus `hits-hq/00-META/how-we-build.md`. Checked against the
design: ops-log as sole source of truth (FR-10/30/31), one surface through
`client` (FR-41), every component a NATS micro service (FR-40), minimal
build (out-of-scope list above), wire contract tested against real NATS —
no conflicts. Scaffolding a versioned constitution is deferred to its own
task; nothing in this spec preempts it.

## Acceptance

- `make check` green — fmt, tidy, build, test (race detector), lint; no
  skipped tests.
- Wire-contract tests run against the embedded server in
  `internal/natstest`; the NATS client is never mocked.
- Demonstrated behaviors, each as a test: concurrent claims on one item
  admit exactly one winner (FR-11); a task without `located-in`, an edit to
  a resolved item, and an unregistered `located-in` are each rejected by
  name (FR-20); block-then-unblock restores the interrupted status (FR-21);
  KV rebuilt by replay matches the KV built live (FR-31); duplicate project
  registration fails (FR-12).
