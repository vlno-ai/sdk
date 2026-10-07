# Approved assessment runs — development candidate

This interface is implemented on the development branch. It is **not published**
in the released Python 0.13.0 or CLI 0.14.0 clients. API and end-to-end qualification
are still in progress; these examples require the matching candidate server.

An approved assessment records the system revision, evaluation scope and human
approval before a run is reserved. A workspace key can consume an approval; it
cannot create or impersonate the human reviewer.

The initial supported application profile is Tiny Notes, `archive-note@1`. Its
final-state checks are narrow: the selected note is archived, and the peer note's
final text and archived state are unchanged. This profile does not attest model
identity or establish what happened between snapshots.

## Prepare and reserve

Readiness has four groups: system, environment, evidence and limits. A successful
readiness check does not reserve capacity, and a reader can see ready checks
without having permission to start a run.

```python
import os
from vlno import Client

client = Client(os.environ["VLNO_ENDPOINT"], os.environ["VLNO_API_KEY"])
ready = client.assessments.prepare(assessment_id, plan_revision_id)

# Save these exact identifiers and the random replay key before submitting.
receipt = client.assessments.admit(
    assessment_id,
    plan_revision_id=plan_revision_id,
    approval_review_id=approval_review_id,
    system_revision_id=system_revision_id,
    idempotency_key=saved_admission_key,
)
run_id = receipt["runId"]
```

The receipt is immutable. It identifies the reserved run, not its current status.
The one-attempt limit applies to each admitted run. While approval remains valid,
separate explicit requests can reserve additional runs; there is no lifetime
attempt quota or aggregate model-spending cap in this profile.

The environment and agent have not started. Connect before `claimBefore`;
the reservation window is at most ten minutes and can be shorter.

If a write response is lost or cannot be verified, the reservation may exist.
`admission_outcome_unknown` and `transport_error_outcome_unknown` preserve the
original `idempotency_key`. Resolve uncertainty by repeating the **same command
and key**, never generating a replacement key. The client does not replay this
write automatically. Explicit authorization and conflict failures remain errors.

## Connect your existing harness

```python
run = client.assessments.get_run(run_id)

# Persist a random, private claim before next(). Reuse it for recovery and finish.
case = run.next(saved_claim, timeout=120)
if case is not None:
    # Invoke your harness once with case.task and case.agent.mcp.
    # Keep the Client, workspace key and saved_claim outside the agent context.
    # After your harness completes:
    run.finish(case.index, saved_claim, agent_status="completed")

run.wait()
current = run.assessment()
```

`get_run()` reads an existing approval-bound run. `next()` is the point where
the server checks current authority and starts preparing the environment.
Only an explicit `run_preparing` response is retried with the same claim and
within the original timeout. Transport uncertainty, finishing, model execution
and tool mutations are not automatically retried.

Recovering a connection does not mean the model should be started again. Your
controller must remember whether it already invoked the harness. Each connection
is scoped to one case; this profile offers MCP and mapped app calls, not a shell.
The recorded model choice is a declaration, not proof of the model actually used.

## CLI

```sh
vlno platform assessments prepare "$ASSESSMENT_ID" --revision "$SCOPE_REVISION_ID"

vlno platform assessments admit "$ASSESSMENT_ID" \
  --revision "$SCOPE_REVISION_ID" --approval "$APPROVAL_REVIEW_ID" \
  --system-revision "$SYSTEM_REVISION_ID" --idempotency-key "$SAVED_ADMISSION_KEY"

vlno platform runs next "$RUN_ID" \
  --claim-file ./private-claim --out ./private-connection.json

# Connect your harness to this MCP bridge; it does not load your workspace key.
vlno mcp --connection ./private-connection.json

# Once the harness is finished, from its separate controller:
vlno platform runs finish "$RUN_ID" --case 0 --claim-file ./private-claim
vlno platform runs wait "$RUN_ID"
vlno platform assessments status "$RUN_ID"
```

Use `--json` for machine-readable receipts and state. The claim and connection
files are private; an existing connection file is never overwritten. After a
failed handoff, retain the claim and retry with a new output-file path.
`admission_outcome_unknown` exits with a failure and reports the saved key;
it does not mean the server rejected the reservation.

## Results and stopping

Current status separates phase, synchronization, cleanup and retained evidence.
An unavailable worker or incomplete evidence is not a passing result. Verified
final-state semantics can accompany only a completed passed/failed recomputation
from the sealed worker receipts; the underlying observations remain worker-trusted.

`run.cancel()` or `vlno platform runs cancel "$RUN_ID"` requests cancellation.
It cannot kill your external model process or undo previous actions. Your harness
controls model execution and spending. The server bounds app access and records
cleanup separately, so a cancellation request is not proof cleanup has completed.

Existing `client.runs` and released lifecycle commands remain available. This
extension reuses their connection, finish, cancellation and wait implementations;
it does not introduce a new runner. Lifecycle status streaming does not by itself
capture every message or shell event from an external harness.
