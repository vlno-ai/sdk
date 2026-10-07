"""Control and observe one organization-owned run."""
import copy
import re
import time

from .. import SDKError
from .agent import PlatformCase
from .validation import _CLAIM, _CASE_TERMINAL, _TERMINAL, _duration, _match, _view

class PlatformRun:
    """Trusted run controller. Never pass this object to the evaluated agent."""
    def __init__(self, client, value):
        self._client = client
        self.info = _view(value)
        self.id = value['id']

    def _request(self, method, suffix='', body=None, *, timeout=None):
        try:
            return self._client._transport.request(method, f'/v1/world-runs/{self.id}{suffix}', body, timeout=timeout)
        except SDKError as error:
            error.run_id = self.id
            raise

    def _update(self, value):
        self.info = _view(value, self.id)
        return copy.deepcopy(self.info)

    def status(self):
        return self._update(self._request('GET'))

    def next(self, claim, *, timeout=330):
        """Wait for one ready assignment using the SAME explicit claim.

        Save the claim in your test controller. Reuse it to recover a lost
        assignment response; recovery must not automatically rerun your agent.
        No tool calls or model callbacks are retried by this SDK.
        """
        _match(_CLAIM, claim, 'case claim')
        deadline = time.monotonic() + _duration(timeout)
        if self._update(self._request('GET', timeout=timeout))['state'] in _TERMINAL:
            return None
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise SDKError('world_ready_timeout', run_id=self.id)
            value = self._request('POST', '/next', {'claim': claim}, timeout=remaining)
            if not isinstance(value, dict) or 'case' not in value:
                raise SDKError('invalid_platform_response', run_id=self.id)
            self._update(value.get('run'))
            case = value['case']
            if case is None:
                if self.info['state'] not in _TERMINAL:
                    raise SDKError('invalid_platform_response', run_id=self.id)
                return None
            if not isinstance(case, dict) or case.get('state') not in {'creating', 'ready'}:
                raise SDKError('world_not_ready', run_id=self.id)
            result = PlatformCase(self._client, self.id, case)
            cases = self.info['result']['cases']
            if result.index >= len(cases) or cases[result.index]['scenario'] != result.scenario:
                raise SDKError('invalid_platform_response', run_id=self.id)
            if case['state'] == 'ready':
                return result
            time.sleep(min(0.25, max(0, deadline - time.monotonic())))

    def finish(self, case_index, claim, *, agent_status='completed', timeout=330):
        _match(_CLAIM, claim, 'case claim')
        if type(case_index) is not int or not 0 <= case_index < 16:
            raise ValueError('invalid case index')
        if agent_status not in {'completed', 'error', 'timeout'}:
            raise ValueError('invalid agent status')
        deadline = time.monotonic() + _duration(timeout)
        self._update(self._request('POST', '/finish', {'case_index': case_index, 'claim': claim,
                      'agent_status': agent_status}, timeout=timeout))
        while True:
            cases = (self.info.get('result') or {}).get('cases', [])
            if case_index >= len(cases):
                raise SDKError('invalid_platform_response', run_id=self.id)
            if cases[case_index]['state'] in _CASE_TERMINAL:
                return copy.deepcopy(self.info)
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise SDKError('case_finish_timeout', run_id=self.id)
            time.sleep(min(0.25, remaining))
            self._update(self._request('GET', timeout=max(0.001, deadline - time.monotonic())))

    def transcript(self, case_index, *, after=0):
        """Read a page of the durable trajectory; sequence cursors are server-issued."""
        if type(case_index) is not int or not 0 <= case_index < 16 or type(after) is not int or after < 0:
            raise ValueError('invalid transcript cursor or case')
        value = self._request('GET', f'/cases/{case_index}/transcript?after={after}')
        if (not isinstance(value, dict) or value.get('schema') != 'vlno.world-transcript/1'
                or value.get('runId') != self.id or value.get('caseIndex') != case_index
                or not isinstance(value.get('events'), list) or type(value.get('nextCursor')) is not int
                or value['nextCursor'] < after or type(value.get('hasMore')) is not bool):
            raise SDKError('invalid_transcript_response', run_id=self.id)
        return value

    def recorder(self, case, claim):
        """Trusted telemetry sink. Keep this recorder outside the evaluated agent."""
        from ..trajectory import TrajectoryRecorder
        _match(_CLAIM, claim, 'case claim')
        if not isinstance(case, PlatformCase):
            raise ValueError('recorder requires an assigned PlatformCase')
        return TrajectoryRecorder(self, case, claim)

    def evidence(self):
        """Archive state is separate from whether observations suffice for grading."""
        value = self._request('GET', '/evidence')
        try:
            if (not isinstance(value, dict) or value['schema'] != 'vlno.world-evidence/1'
                    or value['runId'] != self.id or value['status'] not in {'pending', 'complete', 'unavailable'}
                    or type(value['evidenceComplete']) is not bool
                    or not isinstance(value['artifacts'], list) or len(value['artifacts']) > 64):
                raise ValueError()
            seen = set()
            for item in value['artifacts']:
                if (not isinstance(item, dict) or type(item['caseIndex']) is not int
                        or not 0 <= item['caseIndex'] < 16
                        or item['kind'] not in {'report', 'snapshot', 'evidence', 'trace'}
                        or not isinstance(item['sha256'], str) or not re.fullmatch(r'[a-f0-9]{64}', item['sha256'])
                        or not isinstance(item['evaluationId'], str) or not re.fullmatch(r'[a-f0-9]{32}', item['evaluationId'])
                        or item['state'] not in {'pending', 'archived'}):
                    raise ValueError()
                ident = (item['caseIndex'], item['kind'])
                if ident in seen:
                    raise ValueError()
                seen.add(ident)
                if item['state'] == 'archived':
                    if type(item['size']) is not int or not 1 <= item['size'] <= 16 * 1024 * 1024:
                        raise ValueError()
                elif item['size'] is not None:
                    raise ValueError()
            if value['status'] == 'complete' and (
                    not value['artifacts'] or any(a['state'] != 'archived' for a in value['artifacts'])):
                raise ValueError()
            if value['status'] == 'unavailable' and value['artifacts']:
                raise ValueError()
        except (KeyError, TypeError, ValueError):
            raise SDKError('invalid_evidence_manifest', run_id=self.id) from None
        return value

    def artifact(self, case_index, kind):
        """Download immutable JSON bytes verified against the archive manifest hash."""
        if type(case_index) is not int or not 0 <= case_index < 16 or kind not in {'report', 'snapshot', 'evidence', 'trace'}:
            raise ValueError('invalid artifact identity')
        manifest = self.evidence()
        entry = next((a for a in manifest['artifacts'] if a['caseIndex'] == case_index and a['kind'] == kind), None)
        if entry is None:
            raise SDKError('artifact_unavailable', run_id=self.id)
        if entry['state'] != 'archived':
            raise SDKError('artifact_not_archived', run_id=self.id)
        try:
            return self._client._transport.artifact(f'/v1/world-runs/{self.id}/evidence/{case_index}/{kind}',
                                                   size=entry['size'], sha256=entry['sha256'])
        except SDKError as error:
            error.run_id = self.id
            raise

    def cancel(self):
        """Request cancellation; use wait() to observe confirmed cleanup status."""
        return self._update(self._request('POST', '/cancel', {}))

    def wait(self, *, timeout=330):
        deadline = time.monotonic() + _duration(timeout)
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise SDKError('run_wait_timeout', run_id=self.id)
            self._update(self._request('GET', timeout=remaining))
            if self.info['state'] in _TERMINAL:
                return copy.deepcopy(self.info['result'])
            time.sleep(min(0.25, max(0, deadline - time.monotonic())))
