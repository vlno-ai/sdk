# VLNO SDK and CLI

Run your own agent against hosted evaluation environments and see its trajectory
and results in [VLNO](https://beta.vlno.ai). Install a small client; environments,
application state, grading and evidence storage run on VLNO infrastructure.

Version **0.12.1**, preview. Python 3.11+ and standalone `vlno` CLI builds for
macOS, Linux and Windows. No Docker or engine checkout is required by customers.

## Start here

1. Install the Python SDK with `python -m pip install vlno-sdk==0.12.1`.
   Get standalone CLI builds from [GitHub Releases](https://github.com/vlno-ai/sdk/releases/tag/v0.12.1)
   and verify them against `SHA256SUMS` before installing.
2. Obtain an organization-scoped VLNO API key and an enabled suite from your VLNO
   contact. Access to the hosted pilot is explicitly provisioned.
3. Follow [the complete tutorial](docs/tutorial.md). The first example uses a
   scripted Notes workflow, so it incurs no model calls.

```python
import os
from vlno import Client

client = Client("https://api.vlno.ai", api_key=os.environ["VLNO_API_KEY"])
run = client.runs.create("pilot-notes@1", revision="my-agent-v1")
print(f"https://beta.vlno.ai/runs/{run.id}")
```

The tutorial covers the full lifecycle, including assignment, your harness,
recording, grading, cleanup, downloading evidence and recovery after interruption.
The snippet above only creates a run.

## Included

- **Hosted Python client:** organization-owned runs and separate scoped agent
  connections for MCP, mapped app operations and optional world shell execution.
- **Go CLI:** create, inspect, assign, finish, cancel and download evidence;
  live stdin capture and cursor-based transcript access.
- **Trajectory recorder:** harness messages, tool events, stdout/stderr and
  process lifecycle, alongside gateway-observed tool requests and responses.
- **Customer result page:** live trajectory, search/filter, outcome/checks,
  observation limits and retained artifacts after world cleanup.

`from closed_world_sdk.platform import PlatformClient` remains compatible.
`from closed_world_sdk import Client` is the older direct-worker administration
client; do not confuse it with `from vlno import Client`, the customer product API.
Worker-only commands require an operator endpoint and worker key. Hosted customer
keys do not grant worker administration, app import or scenario authoring.

## Development

```sh
PYTHONPATH=python/src:python/tests python -m unittest discover -s python/tests
(cd cli && go test ./...)
```

The clients do not contain environments, graders, customer data or provider keys.
This preview does not claim automatic access to an agent's un-emitted internal
state. Configure its normal transcript output or emit events from your harness.
See [recording coverage](docs/trajectory.md).
