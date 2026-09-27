# Live trajectories

## What is captured

All new hosted gateway requests persist a submission before forwarding it, and
persist the returned body afterward. This includes app calls, MCP messages, and
`exec` / the MCP `world_exec` tool, including returned stdout, stderr and exit
status. A tool request and response share a `callId`. Large responses are split
into ordered parts with a `last` marker; they are not silently truncated by this
recorder. The underlying world tool may itself return `truncated: true`.

The trusted Python recorder adds user/assistant messages, arbitrary tool events,
local harness stdout/stderr, command invocation, exit status and interruption
gaps. `record_process()` captures both streams; enable your harness's normal
streaming transcript output to include its chat and local tools. It cannot
capture data that the harness never emits. Remote calls made outside VLNO are
visible only when your harness records them.

```python
recorder = run.recorder(case, claim)
recorder.message("user", case.task)
recorder.message("assistant", "An actual message emitted by your agent")
recorder.emit("tool_call", {"name": "search", "arguments": {"query": "release"}})
recorder.emit("tool_result", {"name": "search", "output": "Actual returned output"})
# Or capture any local executable without a provider-specific wrapper:
exit_code = recorder.record_process(["python", "my_harness.py"], timeout=300)
# The default stdin document contains task + scoped connection, not owner credentials.
```

Keep `run`, the recorder, the claim and account key in the trusted controller.
Only `case.agent` / `case.agent.connection()` goes to the evaluated harness.
The recorder's child environment is allowlisted (`PATH`, `LANG`, `TMPDIR` by
default). Pass required non-controller variables explicitly with `env_keys=`.
This is not a sandbox for the customer process: run untrusted agents in your
own isolated runtime. The remote world shell is isolated separately.

The Go CLI can stream emitted text or native JSON from stdin:

```sh
my-harness --its-normal-streaming-option |   vlno platform runs record "$RUN_ID" --case 0 --claim-file .private/claim --json
```

The flag above is illustrative: use your harness's documented streaming flag.
The pipe records stdout. Use the Python process recorder to retain stderr as a
separate stream. Native JSON is retained as emitted; the CLI does not infer
vendor-specific roles or convert console text into verified model messages.

## Live reading and replay

Open the run in beta to see the trajectory. Polling runs every three seconds
while active; history survives cleanup. Search and filter entries, expand
inputs/outputs, pause updates, and download the loaded trajectory.

`run.transcript(case_index, after=cursor)` and
`vlno platform runs transcript RUN_ID --case 0 --after CURSOR` return at most
100 events, a `nextCursor`, and `hasMore`. Continue from the returned cursor.
Cursors order committed receipts within the run; source timestamps are separate
because client clocks can differ. The browser loads all available pages before
enabling its download control.

Owner HTTP routes:

- `GET /v1/world-runs/{run}/cases/{index}/transcript?after=0`
- `POST /v1/world-runs/{run}/cases/{index}/transcript`
- `GET /v1/world-runs/{run}/cases/{index}/observations`

Uploads need the organization key (`runs.create`) and the exact active case
claim. A batch contains `claim` and `events`; each event has a UUID `id`, `kind`,
UTC ISO `occurredAt` and object `data`. Kinds are message, tool_call, tool_result,
shell_command, shell_output, stdout, stderr, lifecycle and gap. Reads require
`runs.read`. Scoped agent credentials cannot access these routes.

## Reliability and interpretation

- Harness uploads and environment observations have separate provenance. Uploaded
  events never determine task/policy grades and cannot claim environment authority.
- Event IDs are immutable and idempotent. Re-upload identical events after a lost
  response; changed content for the same ID is rejected. The Python recorder keeps
  an unacknowledged event in memory; `flush()` retries that ID. It does not retain
  a durable local spool after controller death and never reruns your agent.
- Flush before `run.finish()`. New harness uploads are rejected after case
  revocation, expiry or cancellation. Identical accepted-event replay remains safe.
- Each source is bounded to 8 MiB and 8,192 events per task. An event is limited
  to 24 KiB; upload batches to 32 events / 64 KiB. Quota or upload errors are
  explicit. An unmatched action or unfinished capture marker signals a gap.
- Common credential fields and Bearer/VLNO key strings are redacted. The Python
  recorder also strips exact account key, world token and claim values. Arbitrary
  secrets in free text are not reliably recognizable; control what your harness emits.
- The environment audit uses the worker trace, or its hash-verified archived
  artifact. Historic runs show only what was recorded then; missing chat and
  response bodies cannot be reconstructed.
