"""Organization-authenticated VLNO API client for customer-owned harnesses.

This module does not run models or subprocesses. Keep PlatformClient/PlatformRun
in the trusted test process; pass only PlatformCase.agent to the agent harness.
"""
import copy
import math
import re
import time
import uuid

from . import SDKError, Transport

_RUN = re.compile(r'cw_[a-f0-9]{32}')
_CLAIM = re.compile(r'[A-Za-z0-9_-]{32,128}')
_REF = re.compile(r'[a-z][a-z0-9-]{0,47}@[1-9][0-9]{0,8}')
_TERMINAL = {'completed', 'failed', 'cancelled'}
_CASE_TERMINAL = {'passed', 'failed', 'error', 'inconclusive', 'invalid', 'not_run'}


def _match(pattern, value, label):
    if not isinstance(value, str) or not pattern.fullmatch(value):
        raise ValueError('invalid ' + label)
    return value


def _duration(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value <= 0:
        raise ValueError('timeout must be a positive finite number')
    return value


def _view(value, ident=None):
    if (not isinstance(value, dict) or not isinstance(value.get('id'), str)
            or not _RUN.fullmatch(value['id']) or ident is not None and value['id'] != ident
            or value.get('engine') != 'closed_world'
            or value.get('state') not in _TERMINAL | {'provisioning', 'pending', 'running', 'cancelling'}):
        raise SDKError('invalid_platform_response', run_id=ident)
    result = value.get('result')
    if result is not None:
        if (not isinstance(result, dict) or result.get('schema') != 'vlno.world-run/1'
                or result.get('engine') != 'closed_world' or result.get('executionState') != value['state']
                or result.get('evaluationStatus') not in {'not_evaluated', 'passed', 'failed', 'inconclusive', 'invalid'}
                or not isinstance(result.get('cases'), list) or not 1 <= len(result['cases']) <= 16):
            raise SDKError('invalid_platform_response', run_id=value['id'])
        for index, case in enumerate(result['cases']):
            if (not isinstance(case, dict) or type(case.get('index')) is not int or case['index'] != index
                    or case.get('state') not in _CASE_TERMINAL | {'pending', 'running', 'finishing'}):
                raise SDKError('invalid_platform_response', run_id=value['id'])
    elif value['state'] != 'provisioning':
        raise SDKError('invalid_platform_response', run_id=value['id'])
    return value


class PlatformAgent:
    """Leased app/MCP/optional shell access. Contains no customer account key."""
    def __init__(self, transport, world_id, manifest):
        self._transport = transport
        self.world_id = world_id
        self._manifest = manifest
        self._path = f'/v1/world-agent/{world_id}'

    def connection(self):
        """Private manifest: do not log it or put it in a model message."""
        return copy.deepcopy(self._manifest)

    @property
    def mcp(self):
        entry = self._manifest['mcp']
        return {'url': entry['url'], 'headers': dict(entry['headers'])}

    def operations(self):
        return self._transport.request('GET', self._path + '/operations')

    def call(self, operation, /, **arguments):
        return self._transport.request('POST', self._path + '/call', {'operation': operation, 'arguments': arguments})

    def exec(self, argv, *, cwd='/home/model', timeout_seconds=30):
        if 'environment' not in self._manifest:
            raise ValueError('shell access is not enabled for this case')
        return self._transport.request('POST', self._path + '/exec',
                                       {'argv': argv, 'cwd': cwd, 'timeout_seconds': timeout_seconds})


class PlatformCase:
    def __init__(self, client, run_id, value):
        try:
            world = value['worldId']
            if not isinstance(world, str) or not re.fullmatch(r'[a-f0-9]{32}', world):
                raise ValueError()
            if (type(value['index']) is not int or not 0 <= value['index'] < 16 or not isinstance(value['task'], str)
                    or not isinstance(value['scenario'], str) or not _REF.fullmatch(value['scenario'])
                    or isinstance(value['expiresAt'], bool) or not isinstance(value['expiresAt'], (int, float))
                    or not math.isfinite(value['expiresAt']) or value['expiresAt'] <= time.time()):
                raise ValueError()
            manifest = copy.deepcopy(value['connection'])
            if (type(manifest['schema_version']) is not int or manifest['schema_version'] != 1
                    or type(manifest['generation']) is not int or manifest['generation'] != 1
                    or manifest['world_id'] != world or manifest['task'] != value['task']
                    or manifest['expires_at'] != value['expiresAt']):
                raise ValueError()
            authorization = manifest['mcp']['headers']['Authorization']
            if not isinstance(authorization, str) or not re.fullmatch(r'Bearer [A-Za-z0-9_-]{32,128}', authorization):
                raise ValueError()
            token = authorization[7:]
            if token == client._transport.api_key:
                raise ValueError()
            for name, suffix in [('mcp', 'mcp'), ('app', 'call'), ('environment', 'exec')]:
                if name == 'environment' and name not in manifest:
                    continue
                entry = manifest[name]
                path = f'/v1/world-agent/{world}/{suffix}'
                if (entry.get('path') != path or 'url' in entry
                        or entry.get('headers') != {'Authorization': authorization}):
                    raise ValueError()
                entry['url'] = client._transport.endpoint + entry.pop('path')
            if manifest['mcp'].get('transport') != 'streamable-http':
                raise ValueError()
        except (KeyError, TypeError, ValueError, AttributeError):
            raise SDKError('invalid_connection_manifest', run_id=run_id) from None
        self.run_id = run_id
        self.index, self.scenario, self.task = value['index'], value['scenario'], value['task']
        self.world_id, self.expires_at = world, value['expiresAt']
        scoped = Transport(client._transport.endpoint, token,
                           ca_file=client._transport.ca_file, timeout=client._transport.timeout)
        self.agent = PlatformAgent(scoped, world, manifest)


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
        from .trajectory import TrajectoryRecorder
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


class PlatformRuns:
    def __init__(self, client):
        self._client = client

    def create(self, suite, revision, *, idempotency_key=None, ttl_seconds=1800, environment=False, harness=None):
        _match(_REF, suite, 'versioned suite')
        if not isinstance(revision, str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._/@:+-]{0,127}', revision):
            raise ValueError('invalid revision')
        if type(ttl_seconds) is not int or not 60 <= ttl_seconds <= 3600 or type(environment) is not bool:
            raise ValueError('invalid lease or environment')
        if harness is not None and (not isinstance(harness, str) or not re.fullmatch(r'[a-z][a-z0-9-]{0,47}', harness) or not environment):
            raise ValueError('registered harness requires an enabled environment')
        key = uuid.uuid4().hex if idempotency_key is None else idempotency_key
        if not isinstance(key, str) or not re.fullmatch(r'[A-Za-z0-9_-]{8,128}', key):
            raise ValueError('invalid idempotency key')
        value = self._client._transport.request('POST', '/v1/world-runs',
            {'suite': suite, 'revision': revision, 'ttl_seconds': ttl_seconds, 'environment': environment,
             **({'harness': harness} if harness is not None else {})}, key=key)
        return PlatformRun(self._client, value)

    def get(self, run_id):
        _match(_RUN, run_id, 'product run ID')
        return PlatformRun(self._client, _view(self._client._transport.request('GET', f'/v1/world-runs/{run_id}'), run_id))


class PlatformClient:
    """Customer API-key client for VLNO's organization-owned world runs."""
    def __init__(self, endpoint, api_key, *, ca_file=None, timeout=200):
        if not isinstance(api_key, str) or not re.fullmatch(r'vlno_live_[A-Za-z0-9_-]{20,256}', api_key):
            raise ValueError('a VLNO customer API key is required')
        self._transport = Transport(endpoint, api_key, ca_file=ca_file, timeout=_duration(timeout))
        self.runs = PlatformRuns(self)
