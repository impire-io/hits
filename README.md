# hits

The HITS product repo: the `hits` CLI, the public Go [`client`](client/)
package, and the `hits-node` micro service.

HITS is a headless, agent-native issue tracking system. The mission, designs,
decisions, and process live in [`hits-hq`](../hits-hq/); this repo carries the
code. Agents: read [AGENTS.md](AGENTS.md) first.

## Layout

| Path | What it is |
|---|---|
| `cmd/hits` | The terminal client (thin main; logic in `internal/cli`). |
| `cmd/hits-node` | The micro-service daemon (thin main; logic in `internal/node`). |
| `cmd/hits-index-search` | The full-text index service (thin main; logic in `internal/index/search`). |
| `cmd/hits-index-semantic` | The semantic index service (thin main; logic in `internal/index/semantic`). |
| `contract` | The shared platform contract: op envelope, item model, invariants, fold. |
| `client` | The public Go client package — the one way callers talk to the service. |

## Build & run

```sh
make check   # fmt + tidy + build + test + lint — the quality gate
make build   # binaries land in bin/
```

Both binaries connect through NATS contexts (`nats context ls`):

```sh
bin/hits-node    # run the service (Ctrl-C to stop)
bin/hits ping    # ask the running service to identify itself
```

## Releases

Pushing a `v*` tag builds and publishes both binaries for every platform via
goreleaser ([release workflow](.github/workflows/release.yml)); the tag's
version is stamped into the binaries. CI runs the same gate as `make check`
on every push and pull request.

## License

[Sustainable Use License](LICENSE) — free for internal business,
non-commercial, and personal use.
