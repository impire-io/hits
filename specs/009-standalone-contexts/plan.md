# Plan 009 — standalone hits contexts

How [spec.md](spec.md) lands. Design authority: `hits-hq` @ `f62dc6a`.

## Layout

```
internal/connect/
  hitscontext.go   the hits context document: oauth top-level, nats subtree
                   as json.RawMessage; load-by-name with the guidance error;
                   the oauth+nats.token conflict check (via natscontext's
                   exported Settings type); the temp-file shim
  resolve.go       name resolution (flag > config default, nothing else) and
                   the directory helpers; the nats CLI dir helper survives
                   for import only
  connect.go       Connect: empty name → default URL; named → load, oauth
                   handler, shim into natscontext.Connect
  config.go        gains saveDefaultContext (read-modify-write preserving
                   unknown fields) for the select verb
  contexts.go      NEW — the management API the verbs are thin over:
                   ListContexts, AddContext, ImportContext, RemoveContext,
                   ContextPath, SelectContext
  auth.go          resolveOAuth reworked to the standalone loader
internal/cli/
  contexts.go      NEW — the `hits context` verb family; $EDITOR spawn
  cli.go           `context` dispatch + usage text; --context help reworded
AGENTS.md          connection non-negotiable reworded to the standalone shape
```

## Mechanics

- **The shim, not a loader.** `natscontext` v0.1.3 exports only
  `Connect(name)` (name or absolute path); nothing builds options from a
  loaded `Settings`. So the seam writes the `nats` subtree to
  `os.MkdirTemp` (0700) + 0600 file, calls `Connect(path)`, and removes
  the directory in a defer. The subtree is raw bytes end to end —
  `json.RawMessage`, never re-marshaled — so unknown fields survive and
  the schema stays upstream's. A missing subtree writes `{}`.
- **The conflict check borrows the schema too**: unmarshal the subtree
  into `natscontext.Settings` and look at `.Token` — no hits-side field
  list to drift.
- **Empty name is the default connection**, `nats.Connect("")` semantics
  (localhost), reached without reading the nats CLI's selection marker.
  The zero-config `hits up` quickstart keeps working.
- **Verbs are thin** over `internal/connect`, like the auth family:
  parse flags, call the management API, print. `add`/`edit` spawn
  `$EDITOR` attached to the terminal; tests set `EDITOR=true` or leave it
  unset (`add` then prints the path; `edit` errors plainly).
- **`select` preserves the config file**: read into a generic map, set
  `defaults.context`, write back — future fields survive.
- **Migration is the import verb**: one `hits context import <name>` per
  existing nats-CLI workflow; the not-found error names it.

## Tests

- **Rework in place** (`internal/connect`): resolution table loses the
  marker fallback and asserts the marker is *ignored* when present;
  the collision test becomes a does-not-resolve test (nats-dir-only name
  fails with the guidance error); oauth validation and all handler/
  refresh/lock tests move to the nested shape via the test helpers.
- **New wire tests**: nested plain context connects against embedded
  NATS; `ImportContext` of a real nats context file connects identically;
  oauth context connects with the handler exactly as before.
- **Verb tests** (`internal/cli`): ls (default marked) / add (scaffold +
  refuse-existing) / import / select (config rewritten, other fields
  kept) / rm / edit-without-EDITOR error — driven through `Run` like the
  auth verbs, guard connector standing watch.

## depguard

No rule changes: the verbs live in `internal/cli` (already allowed to
import the seam), the management API in `internal/connect` (imports
stdlib + natscontext only). The `connect-is-shared-bootstrap` fence
holds as is.
