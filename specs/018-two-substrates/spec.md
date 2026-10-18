# Spec 018 — The client speaks two substrates

**Source:** hits-hq decision 0023; `02-DESIGN/the-client.md` and `mcp-server.md` as amended (2026-10-18). **Tracker:** hits-10. **Release:** 0.10.0.

## What

The `hits` CLI and `hits-mcp` reach a HITS on chronicle (release 0.8) or a fleet of release 0.7.0, frozen, through one client surface; the context a caller dials chooses.

## Requirements

1. `client` is an interface over the request and reply types; `client/chronicle` implements it on chronicle (unchanged from 0.8), `client/fleet` on the 0.7 wire contract (`hits.api.*`, `hits.graph.*`, `hits.search.query`, `hits.semantic.query`, `hits.ping`), the actor given at construction.
2. `--context NAME` resolves against chronicle's contexts first, then hits's own; a name both hold is refused; with no name, chronicle's selection, else hits's default context, else a refusal naming both ways to get one.
3. On a chronicle context an actor named by `--actor` or `HITS_ACTOR` is refused; on a hits context a write needs one — the flag, `HITS_ACTOR`, the hits config's default — and the node validates it.
4. `hits context`, `hits auth` return as 0.7 had them; `hits ping` answers on both substrates (the node's name and version; the store's announcement).
5. `store init`, `migrate` and `audit --since-seq` are chronicle's alone and refuse a hits context.
6. The fleet is proven against the v0.7.0 binaries built from their tag by the test harness, on the embedded NATS; a test that cannot build them fails. Nothing of the fleet returns to the repository.
7. The same surface suite runs on both substrates.

## Not in scope

A fix or feature for the fleet; a second record of the tracker; a flag that names the substrate.
