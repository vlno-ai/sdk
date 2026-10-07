# Run and recover an assessment — development candidate

This guide describes the D01 development candidate, not a published SDK or CLI
release. It requires the matching candidate API, including its current-identity
endpoint. End-to-end client qualification is still in progress.

A session connects your existing agent to an approved assessment. It saves the
run reservation, scoped connection identity and emitted transcript so you can
recover an interrupted controller without starting the agent again.

## Before you start

Choose the assessment and its exact approved scope revision. Register the system
version you intend to assess and complete its scope review first. Preparation
shows blockers before reserving a run; it does not grant reviewer authority.

Configure the API endpoint and workspace key through your existing VLNO client
configuration or secret manager. Keep the key in the controller, outside the agent.
Use a new session directory on a private local APFS or ext4 filesystem. Network,
cloud-synced and Windows session storage are unsupported in this candidate.
Do not add session directories to source control: they contain a private claim
and potentially sensitive agent output.

The initial application profile is Tiny Notes. Its final-state checks verify
that the selected note was archived and the peer note’s final text and archived
state are unchanged. A
successful run of this profile is not a general agent safety or security score.

## Start from the CLI

```sh
vlno --wait-timeout 10m sessions start ./private-run \
  --assessment "$ASSESSMENT_ID" --revision "$SCOPE_REVISION_ID" \
  --agent-timeout 300 -- python3 ./my_harness.py
```

The command is executed directly, once. Your harness receives one JSON line on
stdin containing `task` and `connection`, using the existing VLNO handoff format.
Use that scoped connection with your harness's existing MCP support. This does
not automatically configure a third-party agent that expects a different input
format. Existing runner integrations remain responsible for their own setup.

Enable the harness's normal conversation or structured stream output to include
its emitted messages and tool activity. Stdout and stderr are captured live;
private state that the harness does not emit cannot appear in the transcript.

Use `--env NAME` for each additional environment variable your harness needs.
Values stay in memory; workspace credentials and the private claim are withheld.
`--agent-timeout` bounds the supervised command and is clipped to the connection's
remaining lease. `--wait-timeout` bounds the overall CLI operation, including
preparation and evidence collection; the example allows ten minutes. The global
`--timeout` option controls individual API requests.
The agent runs on your machine under your account; this recorder is not a local
security sandbox or a model-spending limit.

## Start from Python

```python
import os
from vlno import Client

client = Client(os.environ["VLNO_ENDPOINT"], os.environ["VLNO_API_KEY"])
result = client.assessments.session(
    "./private-run",
    assessment_id=assessment_id,
    plan_revision_id=scope_revision_id,
).run_command(["python3", "./my_harness.py"], timeout=300)

print(result["phase"], result["outcome"])
```

Constructing the session object does not create a run. `run_command` prepares the
assessment, saves its reservation request, connects, supervises the command,
records the finish declaration and observes the result. It closes its local lock
on return or error. A process exit of zero is a completion declaration; the
assessment's retained evidence determines its outcome.

## Recover after an interruption

Keep the original session directory. Do not start again with a different path
just because a response was lost: the original reservation may already exist.

```sh
vlno sessions status ./private-run
vlno sessions resume ./private-run
```

```python
with client.assessments.open_session("./private-run") as session:
    current = session.resume()
```

Resume settles saved requests and uploads saved transcript events with their
original identities. It never starts another process or repeats an application
operation. A session that was only prepared remains unsubmitted; Python raises
`SessionError` with code `session_admission_unconfirmed`. If the original
process's exit was not recorded, its state remains unknown; resuming cannot
reconstruct missing output or signal a process using a historical PID.

`SessionError` is exported from `vlno`. Its `session_path` locates the saved run
and `recovery_code` describes what remains uncertain. Keep that directory when
handling an error; do not turn an exception into an automatic new run.

Read the result as separate facts: assessment outcome, process observation,
transcript completeness, retained evidence and environment cleanup. Incomplete
capture stays visible even if the finish request was already accepted.

The endpoint, workspace, approved source and claim owner are fixed for recovery.
A different key identity cannot take over a claim. Another authorized workspace
reader can inspect the run, and an authorized operator can request cancellation.

## Connect a harness you control

```python
with client.assessments.session(
    "./private-external-run",
    assessment_id=assessment_id,
    plan_revision_id=scope_revision_id,
) as session:
    case = session.connect()
    if case is not None:
        # Invoke your existing harness once with case.task and case.agent.mcp.
        run_my_harness(case.task, case.agent.mcp)
        current = session.finish(agent_status="completed")
```

Here completion is your explicit declaration. The controller has not observed
the external process's full output or exit, so capture remains incomplete.
Leaving the context only closes the local lock; it does not finish or cancel.
Report `error` or `timeout` explicitly when those are the observed outcome.

## Stop an assessment

```sh
vlno sessions cancel ./private-run
```

Cancellation requests app-access revocation and environment cleanup. It does not
prove cleanup has finished, stop an independently running external harness, or
undo its actions. While a controller owns the session, another client cannot
write to it. Use the live controller to stop its owned process, or the existing
run cancellation API to revoke access separately.

Never edit or reset the journal to clear an error. A corrupt, incomplete or
unsupported session directory fails closed. A busy session must be released by
its current owner before another client can use it.
