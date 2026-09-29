# VLNO hosted clients: hosted evaluations and live trajectories

This guide covers the runnable customer path. It supersedes earlier conceptual
SDK examples: the released client is synchronous and uses organization-owned
suite runs, not an async `World.create()` interface. Current hosted admission is
an explicitly enabled pilot; publishing the client does not open the service to
unprovisioned organizations.

## 1. Install

Requirements: Python 3.11+ for the SDK, or the standalone CLI for your OS/CPU.
Install the SDK from PyPI. CLI archives, a wheel for offline installation, source
examples and `SHA256SUMS` are in the [CLI 0.14.0 release](https://github.com/vlno-ai/sdk/releases/tag/v0.14.0). The Python SDK remains 0.13.0.

```sh
python3 -m venv .venv
. .venv/bin/activate
python -m pip install vlno-sdk==0.13.0
python -c 'import vlno; print(vlno.__version__)'
```

For the CLI, extract the matching `vlno_0.14.0_OS_ARCH` archive and put `vlno`
(or `vlno.exe`) on PATH. Check the downloaded file's SHA-256 against SHA256SUMS.
On macOS/Linux use `shasum -a 256 FILE` or `sha256sum FILE`; Windows PowerShell
has `Get-FileHash FILE -Algorithm SHA256`. `vlno version --json` must show 0.14.0.

Python source installation is also supported from this client-only repository:
`python -m pip install ./python`. Building the CLI needs Go 1.23+:
`cd cli && go build -trimpath -o vlno ./cmd/vlno`.

## 2. Access and credentials

Sign in to [VLNO beta → API keys](https://beta.vlno.ai/api-keys) and select the
workspace you will evaluate in. An owner or admin can choose **Create key**, give
it a name, select **Operator — run evaluations**, and set a 7, 30 or 90 day expiry.
Copy the full key immediately: it is shown once and cannot be retrieved after
the dialog closes. Other members should ask a workspace owner or admin for a key.

The page lists key prefixes, access, status, expiry and last use. **Revoke** stops
API access for that key; create a replacement if needed. Read-only keys can view
results but cannot start evaluations. The Harness page's session token is a
separate credential and cannot replace the account API key.

Use `https://api.vlno.ai` for the product API. Keys apply to their workspace;
creating one does not enable additional suites. Hosted access is provisioned per exact suite version and worker. Inspect
[Hosted suites](https://beta.vlno.ai/hosted-suites), `client.suites.list()`, or:

```sh
vlno platform suites list --json
```

An empty list means no suites are assigned to this workspace. `pilot-notes@1`
is the smoke test. `notes-integrity@1`, when assigned, checks targeted changes
and preservation of unrelated Notes records during the workflow. See
[suite scope and connection qualification](hosted-suites.md). The native Gitea
`issue-instruction-conflict@1` suite, when assigned, runs a clean/challenge comparison
with a conflicting peer review. Follow [paired evaluations](paired-evaluations.md)
and use a fresh harness session for each of its two cases.

Set `VLNO_API_KEY` from your secret manager or a private local file; do not put
it in source code, shell history, screenshots, messages or the model prompt.
For a trusted terminal, this reads without echoing the key:

```sh
export VLNO_ENDPOINT=https://api.vlno.ai
read -rs VLNO_API_KEY; export VLNO_API_KEY
```

The `read` syntax is for bash. PowerShell users should use their normal secret
manager to populate the process environment. The Python examples read this
variable. The CLI can instead save its private configuration:

```sh
vlno --endpoint https://api.vlno.ai platform login --key-stdin < /path/to/private-key
```

The account key controls runs. Each assigned task has a separate, expiring agent
capability. Pass only that scoped connection to your agent, never the account
key, case claim, whole run object or recorder.

## 3. Run the first hosted smoke test

Download/extract the release source archive to get `examples/`. From its root:

```sh
python examples/quickstart.py --state .vlno-demo-state.json
```

This launches one ordinary scripted Notes workflow through a local subprocess.
No model is called. It prints a beta URL immediately, captures process output
while the harness calls the real hosted app, requests independent grading, and
waits for confirmed cleanup. Open that URL while it runs.

Expected final output: evaluation `passed`, cleanup `confirmed`. The result page
shows the task, harness output, observed app inputs/responses, checks and
artifacts. The recovery file is private and includes the case claim; do not
upload it. A second invocation with the same state filename intentionally stops
instead of rerunning actions. See recovery below.

Provisioning and cleanup take variable time; allow several minutes for the
complete hosted workflow. The example allows up to 600 seconds for lifecycle
operations. This is not a latency SLA.

## 4. Connect your own harness

The trusted test controller creates and claims a run:

```python
import os, secrets
from vlno import Client

client = Client('https://api.vlno.ai', os.environ['VLNO_API_KEY'])
request_key = secrets.token_hex(16)  # Persist before submitting creation.
run = client.runs.create('pilot-notes@1', revision='my-agent-v1',
                        idempotency_key=request_key, environment=True)
claim = secrets.token_urlsafe(32)   # Persist privately before requesting a case.
case = run.next(claim, timeout=600)
```

These snippets illustrate the interface; use the runnable quickstart's recovery
file pattern in an actual controller. Every case requires a new claim.

- HTTP MCP: give your agent `case.agent.mcp` (`url` plus scoped `headers`).
- Direct app calls: `case.agent.call('list_notes')`.
- Remote shell: `case.agent.exec(['python3', '-c', 'print("hello from the world")'])`.
  The run must have been created with `environment=True`; MCP exposes the same
  shell as `world_exec`.
- Agent input: `case.task`.

Your harness chooses its own model/provider, prompt and tooling. VLNO hosts the
world and evaluates resulting state/observations. You do not implement grading
hooks. Shell activity in your local agent runtime differs from the remote world
shell; capture local tools through your harness transcript.

For a local executable that accepts the default JSON stdin contract:

```python
recorder = run.recorder(case, claim)
exit_code = recorder.record_process(
    ['python', 'my_harness.py'],
    env_keys=['HOME', 'ANTHROPIC_API_KEY'],  # Only if this harness needs these.
    timeout=300,
)
run.finish(case.index, claim,
           agent_status='completed' if exit_code == 0 else 'error', timeout=600)
report = run.wait(timeout=600)
assert report['evaluationStatus'] == 'passed'
assert report['summary']['cleanup']['status'] == 'confirmed'
```

`my_harness.py` reads `{task, connection}` from stdin and connects its agent to
that scoped MCP/app/shell connection. `examples/notes_harness.py` demonstrates the
contract without a model. Existing agent CLIs need their usual configuration for
MCP and prompt input; the recorder captures their output but does not invent a
universal launch flag for Claude Code, Codex or another CLI. Supply `input=` when
your executable expects a different JSON stdin document.

For an in-process harness, forward actual messages/events to
`recorder.message(role, text)` or `recorder.emit(kind, data)` as they occur.
The recorder is a telemetry hook, not a customer-defined eval hook. Flush all
recording before finish. The [trajectory guide](trajectory.md) explains kinds,
provenance, redaction, limits and failure handling.

## 5. CLI lifecycle

```sh
umask 077
mkdir -p .private
vlno platform runs create pilot-notes@1 --revision my-agent-v1 \
  --idempotency-key my-persisted-request-key --environment --json
# Copy the returned cw_... identity into RUN_ID; this is not a secret.
export RUN_ID=cw_REPLACE_WITH_RETURNED_ID
vlno platform runs next "$RUN_ID" --claim-file .private/claim \
  --out .private/connection.json --wait-timeout 600s --json
```

Use a new claim and connection file for each task. The returned output includes
`case_index`; the following examples assume the first task, index 0.
To connect a stdio MCP consumer, register this command as its MCP server:

```sh
vlno mcp --connection /absolute/path/to/.private/connection.json
```

Stream emitted harness stdout/native JSON into the live trajectory:

```sh
# Run your configured harness in its isolated runtime and pipe its stream here.
vlno platform runs record "$RUN_ID" --case 0 --claim-file .private/claim --json
```

That command reads stdin until EOF. It does not launch an agent. The Python
process recorder is the simplest way to capture stdout and stderr separately.
When the harness is done, finish the task and wait:

```sh
vlno platform runs finish "$RUN_ID" --case 0 --claim-file .private/claim \
  --agent-status completed --wait-timeout 600s --json
vlno platform runs wait "$RUN_ID" --wait-timeout 600s --json
vlno platform runs transcript "$RUN_ID" --case 0 --after 0 --json
vlno platform runs evidence "$RUN_ID" --json
vlno platform runs artifact "$RUN_ID" --case 0 --kind trace \
  --out ./trace.json --json
```

`wait` exits 0 only for a passed, completed run with confirmed cleanup. A finished
agent process is not a passing evaluation. Trace artifact download waits on the
archive's availability: check `evidence` until it is `complete`, then download.
The new live trajectory includes uploaded messages and response bodies; the
sealed `trace` artifact remains the independent worker call audit.

## 6. Read the result correctly

Open `https://beta.vlno.ai/runs/RUN_ID`.

- **Task outcome** says whether the required app state was achieved.
- **Policy checks** reflect the declared scenario checks and available evidence.
- **Agent trajectory** exposes the recorded conversation/actions/responses.
- **Evidence limits** stay visible for missing or insufficient observations.
- **Run details** expose operational status and identities only when needed.
- **Evidence and downloads** hold hash-verified retained artifacts.

`completed` describes execution, not success. `inconclusive` means required
observations were insufficient; `invalid` indicates an infrastructure/evaluation
problem. The Notes smoke test checks its declared workflow; it does not establish
broad security capability or suitability of every agent.

## 7. Recovery and troubleshooting

- Lost create response: repeat the same request with the same persisted
  idempotency key and payload. A new key creates another run.
- Assignment timeout: reuse the same claim to recover that assignment. Do not
  generate a new claim or automatically rerun the harness.
- Tool timeout: the mutation may already have happened. Inspect state/trajectory;
  do not blindly repeat tool writes.
- Lost transcript acknowledgement: the Python recorder retains the pending event
  ID in memory. `recorder.flush()` retries it; it does not survive process death.
- Lost finish response: repeat the identical finish request/claim/status, then
  inspect `run.status()` / `vlno platform runs status`. Finish is replay-safe.
- Controller interruption: use the private recovery file to retrieve the existing
  run. Inspect its phase before deciding whether to finish or cancel; never
  silently restart the agent. `run.cancel()` requests cleanup; `wait()` observes it.
- HTTP 401: expired/revoked/wrong account key, or a scoped capability used after
  finish. HTTP 403: insufficient grants. Pilot admission errors: organization is
  not enabled or its worker configuration is unavailable.
- No chat entries: enable the harness's emitted transcript or forward messages.
  Older runs cannot recover messages they never recorded.
- Recording gap/unmatched action: preserve the uncertainty. It may be a running
  action or failed capture; it is not proof that the action failed.
- Archive pending: grading and background evidence archival are separate. Check
  again; receipt-backed downloads work after cleanup and worker unavailability.

Never email credentials or private connection/recovery files. Share the run URL,
client version, non-secret error code and relevant timestamp with VLNO instead.


## 8. Retention and deletion

Hosted data remains on VLNO infrastructure: the product database stores run
metadata, results and the combined transcript; the worker retains execution
receipts, observations and task files; the evidence archive stores report,
snapshot, evidence and trace objects. World cleanup ends execution and revokes
the agent connection. It does not by itself erase these retained records.

Reading run data requires `runs.read` in the owning workspace. Read-only,
Operator and Owner/Admin roles can read it; another workspace and a scoped
agent connection cannot. Authorized VLNO infrastructure administrators also
operate the underlying storage. This is application-level tenant isolation,
not a claim that VLNO operators cannot access stored data. Confirm deployment
region and contractual data requirements with VLNO before submitting sensitive
workloads; client installation does not select a region or residency policy.

Open [Evaluation data](https://beta.vlno.ai/data-controls) to see the workspace
policy and each run's expiry. Owners and admins can choose 7, 30 or 90 days, or
keep data until explicitly deleted. Saving applies only to newly created runs;
existing runs retain their original policy. Retention begins when execution ends,
not when the short agent lease expires. An unset policy keeps data indefinitely.

To delete a run, open its data status, choose **Delete run data**, and enter the
complete run ID. The run must be terminal with confirmed resource cleanup.
Acceptance removes access immediately; worker files, all archived object versions,
and database payloads are then deleted in retryable stages. Status remains
**Deletion in progress** until every live-data stage completes. Minimal identifiers,
idempotency hashes and audit records remain to prevent accidental recreation.
Downloaded copies are your responsibility. The backup expiry estimate remains
unknown until the entire backup policy has been verified; live-data deletion does
not mean every backup has been erased.

Read status with an ordinary operator key:

```python
policy = client.data.policy()
status = client.data.status(run.id)
requests = client.data.deletions(limit=50, offset=0)
print(status["state"])  # retained, deleting, or live_data_deleted
```

```sh
vlno platform data policy --json
vlno platform data status "$RUN_ID" --json
vlno platform data deletions --limit 50 --offset 0 --json
```

Policy changes and deletion require `org.manage`; the operator key issued by the
beta onboarding flow deliberately lacks that permission. Prefer the authenticated
owner/admin page for those actions. If your trusted administrative automation has
an explicitly issued owner/admin API key, the equivalent calls are:

```python
policy = client.data.policy()
client.data.set_policy(30, expected_revision=policy["revision"])
client.data.delete(run.id, confirm_run_id=run.id)
```

```sh
vlno platform data set-policy --days 30 --revision CURRENT_REVISION --json
vlno platform data delete "$RUN_ID" --confirm-run "$RUN_ID" --json
```

Do not give an administrative key to an evaluated agent. A 409 on policy update
means someone changed the policy; read it again before saving. After a deletion
request times out, read data status before retrying the same confirmed request.
Repeated requests are safe. Normal run/evidence/transcript reads return 410 once
deletion is accepted; data status remains available. Reusing the old creation key
will not recreate deleted workloads.

## 9. Permissions and key replacement

The run controller needs `suites.read`, `runs.create`, `runs.read`, and
`runs.cancel`. The Operator role includes all four. Read-only access cannot run or
cancel evaluations. API-key issuance/revocation and data-management mutations
require `org.manage` (Owner/Admin). Suite assignment additionally requires VLNO
staff authorization; workspace owners cannot grant themselves private suites.

To replace a key, create a new named Operator key in **API keys**, update the
controller's secret, verify suite discovery, then revoke the old key. Revocation
stops account API access; existing scoped agent leases are separate. Cancel an
active run to revoke its capability and clean up its world. Removing suite access
blocks new runs and new task assignments, but does not retroactively stop a task
already assigned. Use cancellation when that is required.

Watch the run from another terminal while the harness works:

```sh
vlno platform runs watch "$RUN_ID" --wait-timeout 900s --json
```

This follows the recorded trajectory across cases. Connection loss resumes from the
last complete event automatically; it does not restart the harness. Save a trajectory
frame's `nextCursor` and pass `--after CURSOR` to resume in a new command.
