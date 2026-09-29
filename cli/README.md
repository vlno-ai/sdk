# VLNO CLI 0.14.0

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

## Hosted catalog and data

`vlno platform suites list` shows the suites assigned to the authenticated workspace.
`vlno platform data policy`, `status RUN_ID`, and `deletions` read retention and
deletion progress. Administrative `set-policy --days 30 --revision N` applies only
to new runs; `delete RUN_ID --confirm-run RUN_ID` explicitly requests irreversible
payload deletion. These two mutations require `org.manage`, not an ordinary
Operator key. See [the tutorial](../docs/tutorial.md) for backup and recovery limits.

## Live runs

`vlno platform runs watch RUN_ID --wait-timeout 900s --json` streams run status
and recorded messages, tool calls, shell activity and results as JSON lines.
`--after CURSOR` resumes trajectory after a previously received `nextCursor`.
Within a command, interrupted connections reconnect automatically without repeating
case claims or actions. `next` and `wait` also use SSE while waiting.

`watch` observes; use `wait` for a pass/fail CI exit code. Streams replay recorded
activity, not private harness activity that was never captured. See
[trajectory documentation](../docs/trajectory.md).
