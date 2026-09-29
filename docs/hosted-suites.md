# Hosted suite scope and integration boundaries

The catalog in your workspace is the authority for exact suite versions, allowed
connections and limitations. Installation does not assign suites. `GET /v1/world-suites`
requires `suites.read` and returns only enabled grants for the current worker.
Creation and case assignment independently verify the grant, worker identity and
suite digest. Operators cannot override these checks with client flags.

| Suite | Purpose | What a pass supports |
| --- | --- | --- |
| `pilot-notes@1` | End-to-end integration smoke test | The requested note was archived and declared final-state checks passed. |
| `issue-instruction-conflict@1` | Paired instruction-conflict evaluation in native Gitea | A successful clean control and separate task/security results for a fixed peer-review conflict. See [paired evaluations](paired-evaluations.md). |
| `notes-integrity@1` | Bounded record-integrity assessment | The target changed as requested, unrelated seeded records retained their required state/content, and monitored archive operations did not successfully modify protected records, including temporary changes. |

Availability depends on workspace assignment. The Notes suites are controlled
integration and record-integrity workflows. The Gitea suite adds a fixed indirect
instruction-conflict evaluation. None is a broad enterprise benchmark or a test
of vulnerability exploitation, exfiltration, general cybersecurity capability or
all possible side effects. Scripted qualification tests the
measurement system; it does not establish any model's performance.

Integrity grading keeps task completion and policy compliance separate. A harness
can finish the target task and still fail policy after changing another record.
Restoring the final state does not remove an observed prohibited operation. If
required observations are incomplete, the result cannot support a clean policy
claim. The exact evidence source and coverage are included in retained reports.

## Bring your own harness

| Path | Customer integration | Qualification boundary |
| --- | --- | --- |
| App operations | Pass the scoped `case.agent` adapter to your tools. Call `operations()` and `call()` for the declared app mapping. | Public Python SDK → product API → real worker/application, with evidence and cleanup. |
| MCP | Use `case.agent.mcp` with an HTTP MCP client, or `vlno mcp --connection /private/connection.json` for a stdio consumer. | The CLI bridge and product gateway protocol are tested. This does not certify every framework or configuration. |
| World shell | Request `environment=True` only when the suite grant allows it; use `case.agent.exec(...)`. | Commands execute in the scoped world shell. A local agent's own shell does not automatically become this shell. |
| Local harness process | Launch your existing command from a trusted controller and use the trajectory recorder for emitted output. | Captures emitted stdout/stderr and process lifecycle; not hidden internal reasoning. Scope the process environment and files so the account key and recovery claims are absent. |

Claude Code, Codex, OpenHands and OpenClaw are examples of possible MCP consumers,
not an unqualified compatibility list. Configure the exact installed version and
run the smoke test. Providers, model authentication and agent-specific startup
remain under customer control. VLNO does not need a bespoke wrapper to expose the
standard app/MCP connection, but connection configuration is still required.

Full trajectory combines gateway-observed activity with harness-emitted messages
and process output. These sources stay labeled. Customer-emitted statements never
replace independent app-state/policy grading. See [trajectory coverage](trajectory.md)
and [the complete tutorial](tutorial.md).
