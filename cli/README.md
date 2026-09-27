# VLNO CLI 0.12.0

A standalone customer client for hosted VLNO evaluations. See the repository
[tutorial](../docs/tutorial.md) and [trajectory guide](../docs/trajectory.md).

```sh
vlno version --json
vlno --endpoint https://api.vlno.ai platform login --key-stdin < /path/to/private-key
vlno platform runs create pilot-notes@1 --revision my-agent-v1 --idempotency-key my-stable-request
```

Keys and case claims belong in the trusted controller. Hand only the exported
scoped connection file to your agent. `vlno mcp --connection FILE` supplies a
generic stdio MCP bridge. It does not load your account configuration.

Run `vlno --help` for commands. `platform` uses the hosted organization API;
non-platform world/catalog/authoring commands use a separate operator worker API.
A hosted customer key cannot administer the worker.

Build: `go build -trimpath -o vlno ./cmd/vlno`. Test: `go test ./...`.
