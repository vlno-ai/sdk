# Changelog

## Hosted service update — 2026-09-29

- Add the workspace-assigned `issue-instruction-conflict@1` native Gitea pair.
- Report clean/challenge task completion, the declared security objective and observed review content separately in beta and result JSON.
- Document fresh harness sessions, app/MCP integration and measurement limits in [paired evaluations](docs/paired-evaluations.md).
- SDK and CLI 0.13.0 remain compatible; this service update does not change their binaries.

## 0.13.0 — 2026-09-29

- Discover exactly versioned suites granted to the current workspace with `client.suites.list()` or `vlno platform suites list`.
- Read and manage prospective data retention; inspect deletion progress and request explicit run-data deletion through `client.data` and `vlno platform data`.
- Keep deletion acceptance separate from completion, validate component timestamps, and retain backup/external-copy limitations in client responses.
- Document workspace access, full operator grants, app/MCP/shell integration boundaries and data lifecycle recovery.

## 0.12.1 — 2026-09-27

- Prepare the initial PyPI distribution and remove temporary publication-pending notices.
- Add manually dispatched, checksum-verified trusted publishing from the VLNO repository.
- SDK and CLI runtime behavior is unchanged from 0.12.0.

## 0.12.0 — 2026-09-27

- VLNO distribution name (`vlno-sdk`) and `from vlno import Client` customer API.
- Hosted suite lifecycle, scoped MCP/app/shell connections, retained evidence.
- Live trajectory reads and claim-bound harness event recording.
- Generic Python subprocess capture for stdout/stderr and lifecycle.
- CLI `platform runs record` and `platform runs transcript`.
- Runnable hosted Notes smoke test and complete customer tutorial.

Older direct-worker imports remain available for existing operator integrations.
Hosted pilot admission is provisioned separately from client installation.
