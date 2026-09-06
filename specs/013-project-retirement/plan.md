# Plan 013 — projects retire

## Layout

```
contract/ops.go            OpRetired, RetiredPayload
contract/model.go          Project.Retired, Project.RetireReason
contract/apply.go          ApplyProject folds retired onto the kept key
contract/check.go          CheckProjectOp: per-op validation, retired-slug
                           registration refused
contract/contract_test.go  fold + check coverage for the new op
internal/node/store.go     retireProject (CAS on the project subject),
                           listProjects drops retired, checkRegistered
                           refuses retired, foldOne routes retired to the
                           project fold
internal/node/handlers.go  retireProject handler
internal/node/node.go      project-retire endpoint
client/items.go            RetireProjectSubject, RetireProjectRequest,
                           RetireProject
client/client_test.go      end-to-end retire coverage over real NATS
internal/cli/projects.go   hits project retire <slug> --reason <r>
internal/cli/cli_test.go   CLI coverage
internal/mcp/items.go      retire_project tool
internal/mcp/mcp_test.go   tool-surface coverage
internal/index/graph/service.go  fold skips OpRetired
```

## Mechanics

- The retire publish uses `Nats-Expected-Last-Subject-Sequence` set to the
  registry snapshot's `Seq` — the same read-validate-append loop as item
  writes, so two racing retires cannot both land; the loser re-reads and
  fails `already-retired`.
- The registry key is kept, never deleted: the snapshot's `Seq` is the
  idempotence marker, and deleting it would let a replayed `registered`
  resurrect the slug. `listProjects` filters on the flag instead.
- `checkRegistered` distinguishes its two refusals: `unregistered-project`
  (never seen) and `retired-project` (seen and retired), so the caller
  knows whether to register or to stop.
- The graph consumer is the one unfiltered ops reader; its fold returns on
  `OpRetired` before reaching the item fold, keeping the display-name
  mapping so referenced history keeps rendering.

## Tests

Contract: pure fold and check tables grow retired cases — fold sets the
flag and keeps the fields, replay at old seq is a no-op, re-registration
and double-retire and empty/over-budget reasons are refused. Node/client
(embedded real NATS, race on): retire → list drops it → located-in write
refused with `retired-project` → re-register refused; a closing
transition on an item already naming the slug still lands. CLI: the new
subcommand round-trips and a missing --reason fails before dialing. MCP:
the tool appears with the write annotation and round-trips.
