# D01 durable client session contract, version 1

Design freeze for [SDK session issue 41](https://github.com/vlno-ai/vlno-api/issues/41). Baseline: SDK `796a39980d1e6db4fab90e1e8d714b18e1c4745a`, API `cc844df7a5f8271360d32c1aa6487e1d588df425`. This is a local implementation contract, not a released API or successful model-run claim.

## Storage and ownership

- Initially support macOS/Linux local filesystems with working `flock` and directory `fsync`; qualify APFS and ext4 first. Windows, network filesystems and unknown filesystem semantics are unsupported and fail explicitly when detected. Qualify filesystem type through the opened directory; cloud-synced local folders remain an explicit unsupported path constraint because filesystem type cannot reliably detect synchronization software. Never silently fall back to a PID lock.
- A session directory is owned by the current UID, mode `0700`. Fixed files: `lock`, `journal.json`, `claim.json`; regular files, same UID, mode `0600`, one hard link, no symlinks. Directory/file checks use opened descriptors, not a check followed by an unrelated path open.
- Python `fcntl.flock(LOCK_EX|LOCK_NB)` and Go `syscall.Flock(LOCK_EX|LOCK_NB)` use the same stable `lock` inode. Keep its descriptor open and close-on-exec. Never replace/unlink the lock, including after a crash. A busy lock yields `session_in_use`; no timeout-based takeover. Hold it across the entire active operation, including supervised execution.
- Each journal mutation increments `revision`, validates the full next document, writes a fresh exclusive same-directory temporary file, flushes and `fsync`s it, atomically replaces `journal.json`, then `fsync`s the directory. API dispatch follows only a successful durable write. An ambiguous local write aborts the operation; reopen and validate before continuing.
- Creation is exclusive: an existing directory is opened only through resume/open, never reset. Generate/write `claim.json` before the initial journal; no network mutation occurs until both are durable. An incomplete directory without a journal fails `session_incomplete`; it never triggers new admission automatically.
- Read a maximum of 2 MiB plus one byte. Reject excess, invalid UTF-8, duplicate decoded keys, unknown fields, unpaired surrogates, non-finite numbers, depth above 16, and integers outside `-9007199254740991..9007199254740991`. Counters are nonnegative; the only negative protocol integer is the signed process exit code. Event data may contain finite fractional numbers and safe signed integers. Unknown keys are allowed only inside validated event `data` objects. JSON object key order is insignificant; arrays, missing fields, nulls and values remain exact. All specified fields are required; nullable fields use explicit null.
- Do not persist account keys, worker tokens, scoped access tokens, authorization headers or environment values. Admission replay IDs are not credentials. The private claim is stored separately below. Scrub known credentials before any event or error becomes durable; unknown secrets in customer output cannot be reliably detected. Private storage is not a sandbox against another process under the same UID.

## Exact journal shape

The following is normative notation, not a new runtime library. `U` is a lowercase UUID (source IDs follow A02); newly generated session, launch, event and admission replay IDs require UUIDv4; `T` is UTC ISO time with exactly three fractional digits; `H` is 64 lowercase hex; `Actor` is `{kind:"human"|"service_key",reference:"sa_"+32 lowercase hex}`. Timestamps are observations; only journal revision/event sequence establish order.

```text
Journal = {
  schema: "vlno.client-session/1", id: U, revision: integer>=1,
  createdAt: T, updatedAt: T,
  endpoint: canonical validated API base URL, orgId: nonempty string<=200,
  source: {assessmentId: U, planRevisionId: U, approvalReviewId: U,
           systemId: U, systemRevisionId: U, templateId: U},
  admission: {state:"prepared"|"unknown"|"confirmed", actor:Actor,
    idempotencyKey:U,
    command:{planRevisionId:U,approvalReviewId:U,systemRevisionId:U},
    receipt: RunAdmission|null},
  claim: {file:"claim.json", sha256:H,
    state:"not_started"|"unknown"|"ready"|"ended", actor:Actor|null,
    case: {index:0,scenario:"archive-note@1",worldId:32 lowercase hex,
           generation:1,expiresAt:T}|null},
  execution: {mode:"supervised"|"external",
    state:"not_started"|"start_unknown"|"running"|"exit_unknown"|
          "exited"|"external_active",
    launchId:U|null, command:Command|null, pid:integer>=1|null,
    exitCode:signed 32-bit integer|null, finishedAt:T|null},
  capture: {state:"not_started"|"open"|"complete"|"incomplete",
    nextSequence:integer>=1, acknowledgedThrough:integer>=0,
    gapRecorded:boolean, events:Event[]},
  finish: {state:"not_started"|"unknown"|"accepted",
    actor:Actor|null, command:{case_index:0,
      agent_status:"completed"|"error"|"timeout"}|null},
  cancellation: {state:"not_requested"|"unknown"|"requested",
    actor:Actor|null},
  observation: {checkedAt:T, value:AssessmentRun}|null,
  recovery: {code:RecoveryCode, occurredAt:T}|null
}
Command = {argv:string[], envKeys:string[], cwd:string}
Event = {sequence:integer>=1, event:{id:U,kind:TranscriptKind,
  occurredAt:T,data:JSON object}}
ClaimFile = {schema:"vlno.client-session-claim/1",sessionId:U,claim:string}
```

`claim` is 32 random bytes encoded as 43 unpadded base64url characters. `claim.sha256` hashes those exact ASCII characters, not JSON serialization. `claim.json` is immutable; missing/mismatched identity or digest is `session_corrupt`. The wire claim/finish/transcript body is constructed by combining this private value with the frozen command. No connection token is stored in the journal or claim file.

Endpoint canonicalization follows shared Python/Go origin rules: HTTP only for localhost/127.0.0.1/::1, HTTPS otherwise; no whitespace, invalid hostname, credentials, path other than /, query or fragment; lowercase host, bracket IPv6 and omit default port. Ports must be 1..65535. `RunAdmission` and `AssessmentRun` are the strict A02 public DTOs; preserve exact validated values. Receipt binds endpoint context, org, admitting actor, key, command and all six source IDs. Current observation binds org, run and source, but `admittedBy` is never interpreted as claim owner. `TranscriptKind` is the existing SDK/server allowlist. `Command` has 1..64 arguments, each <=4096 UTF-8 bytes, total <=32768 bytes; 0..64 unique valid environment names, no control credential names; absolute cwd <=4096 bytes. Reject an argument containing any known control credential before persisting the command; do not redact command arguments and then execute a different command. No shell-string interpretation. Values of permitted environment variables are resolved in memory at launch, not serialized.

`RecoveryCode` is exactly: `admission_unknown`, `assignment_unknown`, `launch_unknown`, `exit_unknown`, `capture_incomplete`, `finish_unknown`, `cancel_unknown`, `authority_changed`, `source_changed`, `connection_expired`, `service_unavailable`, `outbox_full`. It carries no remote body, arbitrary exception text or credential. Filesystem corruption/unsupported-platform/busy errors are returned without rewriting a journal that cannot be trusted.

## Cross-field validation and immutable boundaries

- `admission.command` equals the matching source fields. Receipt is nonnull iff admission is confirmed. No claim action before confirmed admission. A prepared admission becomes unknown durably before every first/recovery dispatch; only an exactly matching receipt confirms it.
- Claim actor is null only while not_started. Case is null until a ready reply; ready requires a nonnull case. Ended may retain a previously known case or null. Case identity and expiry, once recorded, cannot change on replay. Convert server expiry to UTC milliseconds by flooring sub-millisecond precision; never round up or extend the effective lease. A new key row is a different actor; it cannot take over an existing claim.
- Run source, endpoint, org, replay key, admission actor/command/receipt, claim secret and bound claim actor are immutable after their respective initial durable intent. No changed approval, system revision or deadline is substituted during recovery.
- `execution.command` is required in supervised mode, null in external mode. Launch ID is null only before supervised start or in external mode. PID is diagnostic only; never use a persisted PID as cancellation authority. ExitCode/finishedAt are present only for a durably observed exit. The exit code is not an evaluator grade.
- `finish.actor` and command are both null iff not_started; otherwise actor equals bound claim actor. First finish intent freezes status/index forever. A later observation cannot rewrite a lost completed/error/timeout declaration. Wire body adds the exact saved claim.
- Cancellation actor is null iff not_requested. It identifies the actual caller requesting cancellation, never an on-behalf attribution. Automatic retry requires that actor; a different actor may independently call the existing low-level cancellation API, then refresh this journal observation. It cannot rewrite or resume the prior actor’s journal cancellation intent.
- Observation is a replaceable current read, not an immutable execution receipt. Never downgrade a confirmed admission or erase an uncertain intent because observation is unavailable. An observation version lower than the saved version is rejected as stale.
- Derived customer phase is computed from these records, not stored redundantly. Validation rejects impossible combinations, including claim before admission, finish before a case, an acknowledged event gap, or complete capture with pending events.

## Durable outbox

- Keep scrubbed event bodies inside this one atomic journal, including acknowledged events for recovery verification. This avoids cross-file outbox/index transactions. Journal maximum remains 2 MiB.
- Each wire event is <=24000 compact UTF-8 JSON bytes (U+2028/U+2029 escaped), matching the existing recorder bound. The single-event envelope with its 43-character claim must also be <=65536 bytes under Python default ASCII JSON encoding/separators, so Go-created events remain recoverable in Python. Lifetime event count <=1024 and serialized wire-event bytes <=1 MiB. Ordinary capture stops at 1000 events or 768 KiB; reserve the remainder for gap/completion/error markers, each <=1024 bytes, generated by the session only. No silent truncation or replacement of previously persisted events.
- Sequences start at 1, are contiguous, unique and immutable. `nextSequence = events.length+1`; IDs are unique lowercase UUIDv4. `acknowledgedThrough` is a contiguous prefix index, never above the final sequence. Persist each event before its first upload.
- Upload one oldest unacknowledged event per request initially, using its unchanged ID, timestamp, kind and data. Validate the exact acknowledgement ID before advancing the prefix with a durable journal write. If response/write is lost, resend the identical event; never regenerate IDs or re-scrub stored payloads.
- Scrub values and keys before persistence; reject key collisions introduced by redaction. Preserve native emitted stream content after documented redaction/chunking; this is controller telemetry, not evaluator evidence or access to un-emitted reasoning.
- The existing recorder's bounded streaming/chunking is reused through a durable sink. Journal failure or ordinary outbox exhaustion stops capture, attempts one reserved gap, and leaves incomplete status. The supervised caller stops its currently owned child on failure; no claim that already performed actions were reversed. External harness stopping is not guaranteed.
- A crash while capture was open means incomplete capture even if all saved events later upload. Pipe bytes not made durable are unknown. Append one durable reserved recovery gap when storage permits; `gapRecorded` prevents duplicate gap creation. A crash cannot be repaired into complete capture.
- Capture is complete only after both stream ends and process exit were observed, the final marker is durable, all events are acknowledged, and no gap occurred. External capture reports only the explicitly recorded stream and remains incomplete in this first schema; it cannot claim a complete external process transcript.

## State transitions and recovery

| Operation | Before network/process side effect | Confirmed result | Recovery rule |
|---|---|---|---|
| Admission | Persist exact key/body/actor and unknown state | Save matching receipt | Explicit resume replays same intent; may perform its first dispatch if crash preceded send |
| Claim | Persist actor and unknown state; verify private claim | Save ready case or ended | Same actor/claim only; typed preparing can poll within original budget; no renewed lease |
| Supervised start | Persist launchId + start_unknown | After spawn, persist running/PID | Any reopened start_unknown/running becomes exit_unknown; never respawn |
| Process exit | Persist observed exit code/time | Begin draining/final telemetry | Missing exit receipt remains unknown even if PID no longer exists |
| Upload | Event already durable | Advance exact acknowledged prefix | Same event body/ID; no model/app retry |
| Finish | Persist exact status/index/actor + unknown | Valid same-run lifecycle acknowledgement makes accepted | Retry identical body only; then observe collecting/terminal |
| Cancel | Persist requested actor + unknown | Save requested, observe cleanup | Repeated same intent; prospective access revocation, not action rollback |

`resume` may settle a previously persisted unknown admission/claim/upload/finish/cancel intent, then observe current status. A merely prepared admission stays unsubmitted: creation of local session files does not authorize resume to reserve a run. It never launches any command, invokes a model callback, repeats an app tool action or silently reserves a replacement run. A persisted pre-dispatch intent may first reach the server during recovery; this is explicit protocol recovery, not proof the original request arrived.

After reopening a supervised start/running record, retain PID as historical information only. Do not signal a possibly recycled PID/process group. Initially, only the original live controller may stop its child through the actual process handle/group it created. If the controller was killed, resume reports uncertain process state and can request app-access cancellation; it cannot claim that the external process stopped.

Before finish, flush all durable events. For supervised execution, if capture is incomplete or process exit uncertain and there is no existing finish intent, require explicit `finish(agent_status="error"|"timeout")` or cancel; never infer completed. For an external harness, an explicit `finish(agent_status="completed")` is permitted as the caller’s declaration only: capture remains incomplete, process exit remains unobserved, and the evaluator independently decides the grade. Resume and context exit never synthesize this declaration. Once a finish intent exists, retry it unchanged even if later telemetry exposes a gap, and display that gap separately. A context manager only releases the lock; it never implicitly finishes, invokes a process or changes an unknown result into success. Interrupt handling in an active supervised operation can request cancel and stop its owned child, with both outcomes separately recorded.

`cancel` with admission unknown has no run ID to target. Report that uncertainty and require explicit same-intent recovery first; do not silently create an admission merely to cancel it. A locked active session cannot be controlled by another journal writer: Ctrl-C acts through the owning controller; a separate authorized low-level API cancellation remains possible. No new local daemon/RPC in D01.

## Identity and authorization dependency

Additive `GET /v1/assessment-runs/access` returns `{schema:"vlno.assessment-run-access/1",orgId,actor}` under current `runs.read` authority. It has no scope/run/worker dependency. This endpoint is implemented in the D01 API candidate; it is not available in the earlier A02 baseline or claimed released. Session use requires read plus the applicable operation grant.

Check current endpoint/org/actor before mutation recovery; the server still enforces time-of-use permission. Admission replay binds its original actor. First explicit claim may use another currently authorized same-org actor, recorded before dispatch; subsequent claim/finish/upload cannot transfer. Read/cancel can remain independently authorized after key loss. A 401/403 or actor mismatch disables retry; it never proves an earlier uncertain write failed. A changed or expired approval blocks new execution, but does not prevent an independently authorized historical read.

## Proposed customer syntax

Syntax is a minimal walkthrough proposal; schema/storage is frozen independently. No additional runner, provider adapter, or automatic process restart.

```python
session = client.assessments.session("./private-run", assessment_id=assessment_id,
                                     plan_revision_id=revision_id)
result = session.run_command(["my-agent", "--stream-json"])
# On interruption, in a later process; no command is accepted by resume:
result = client.assessments.open_session("./private-run").resume()
```

`run_command` prepares, saves admission, claims, executes exactly one explicit command, records, saves finish intent and observes retention. Existing session paths are rejected by this creation path. Stdin receives the existing task/scoped-connection shape. Errors print the safe session path and a recovery command, not control credentials. Environment values are not command-line arguments.

```sh
vlno sessions start ./private-run --assessment "$ASSESSMENT" --revision "$REVISION" -- my-agent --stream-json
vlno sessions resume ./private-run
vlno sessions status ./private-run
vlno sessions cancel ./private-run
```

External harnesses use explicit `session.connect()` and `session.finish(agent_status=...)`, with a private ephemeral scoped connection file if needed. `connect` never starts the harness. A CLI handoff command can be added to the same session family after this walkthrough; no claim that arbitrary external transcripts are automatically captured. Expired connection files are unlinked best-effort, without a physical-erasure guarantee.

## Required shared acceptance fixtures

1. Golden valid/invalid journals and claim files read in Python and Go: duplicate keys, bounds, Unicode, state contradictions, null/absence, immutable source and acknowledgements.
2. Python owns lock/Go rejects, reverse direction, process exit releases lock, child does not inherit it, unsupported filesystem/platform fails explicitly.
3. Crash cut points around file and directory fsync, admission/claim dispatch/response, spawn/exit record, event append/ack, and finish dispatch/ack; no second invocation.
4. Cross-language exact admission/claim/finish recovery and same-ID event replay after lost replies. No fresh identities, automatic actor transfer or rebase.
5. Reused PID cannot be signalled; capture loss is explicit; outbox exhaustion retains existing events and reserves a gap; cancellation/cleanup/grade remain separate.
6. Same-user harness isolation is not claimed. Known account/worker/claim/scoped credentials never enter journal event bodies, stdout diagnostics or child environment; unknown customer secrets remain a documented limitation.

The initial contract was authored without execution. Storage, validation, identity and capture are implemented in the development candidate; the customer workflow below remains under integration. Test and release status belong to the pull request, not this design contract.
