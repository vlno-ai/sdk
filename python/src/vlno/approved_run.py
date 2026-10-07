"""Approved run controller using the existing lifecycle and scoped connection types."""

import copy
import time

from closed_world_sdk import SDKError
from closed_world_sdk.platform import PlatformRun

from .assessment_status import assessment_status


class ApprovedRun(PlatformRun):
    """Keep this controller outside the evaluated agent; hand over only case.agent."""

    def _request(self, method, suffix="", body=None, *, timeout=None):
        if method != "POST" or suffix != "/next":
            return super()._request(method, suffix, body, timeout=timeout)
        # PlatformRun.next already validates the claim and passes its remaining budget.
        budget = self._client._transport.timeout if timeout is None else timeout
        deadline = time.monotonic() + budget
        command = copy.deepcopy(body)
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise SDKError("world_ready_timeout", run_id=self.id)
            try:
                return super()._request(method, suffix, command, timeout=remaining)
            except SDKError as error:
                if error.code != "run_preparing" or error.status != 503:
                    raise
            # Only explicit pending readiness is retried, always the original claim.
            # Unknown transport outcomes, finishing and agent tool calls are never retried.
            time.sleep(min(0.25, max(0, deadline - time.monotonic())))

    def assessment(self):
        """Read current approved-source, evidence and cleanup status separately."""
        return assessment_status(self._request("GET", "/assessment-run"), self.id)
