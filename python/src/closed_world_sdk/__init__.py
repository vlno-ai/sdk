"""Dependency-free remote client. No harness, subprocess, Docker, or VM imports."""
import json
import hashlib
import math
import re
import ssl
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import Request, build_opener, HTTPRedirectHandler, HTTPSHandler, ProxyHandler
import uuid
import secrets
from typing import NamedTuple

__version__ = '0.12.1'


class SDKError(Exception):
    def __init__(self, code, *, status=None, world_id=None, job_id=None, run_id=None, idempotency_key=None):
        self.code, self.status = code, status
        self.world_id, self.idempotency_key = world_id, idempotency_key
        self.job_id = job_id
        self.run_id = run_id
        super().__init__(code)


class NoRedirects(HTTPRedirectHandler):
    def redirect_request(self, *_):
        return None


class Transport:
    def __init__(self, endpoint, api_key, *, ca_file=None, timeout=200):
        parsed = urlsplit(endpoint)
        if parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path not in ('', '/') or not parsed.hostname:
            raise ValueError('endpoint must be an HTTP(S) origin')
        if parsed.scheme != 'https' and not (parsed.scheme == 'http' and parsed.hostname in {'127.0.0.1', 'localhost', '::1'}):
            raise ValueError('HTTPS is required except on loopback')
        self.endpoint, self.api_key, self.timeout = endpoint.rstrip('/'), api_key, timeout
        self.ca_file = ca_file
        context = ssl.create_default_context(cafile=ca_file)
        self.opener = build_opener(ProxyHandler({}), NoRedirects(), HTTPSHandler(context=context))

    def request(self, method, path, payload=None, *, key=None, timeout=None):
        headers = {'Authorization': 'Bearer ' + self.api_key, 'Content-Type': 'application/json'}
        if key:
            headers['Idempotency-Key'] = key
        data = None if payload is None else json.dumps(payload).encode()
        if data and len(data) > 65536:
            raise ValueError('request exceeds 64 KiB')
        req = Request(self.endpoint + path, data=data, method=method, headers=headers)
        try:
            try:
                response = self.opener.open(req, timeout=self.timeout if timeout is None else min(self.timeout, timeout))
            except HTTPError as error:
                response = error
            with response:
                raw = response.read(4 * 1024 * 1024 + 1)
                if len(raw) > 4 * 1024 * 1024:
                    raise SDKError('response_too_large')
                value = json.loads(raw)
                if not 200 <= response.status < 300:
                    details = value.get('error') if isinstance(value, dict) else None
                    code = details.get('code') if isinstance(details, dict) else None
                    if (not isinstance(code, str) or not re.fullmatch(r'[a-z][a-z0-9_]{0,63}', code)
                            or self.api_key in code):
                        code = 'http_error'
                    raise SDKError(code, status=response.status, idempotency_key=key)
                return value
        except (OSError, URLError, ValueError) as error:
            raise SDKError('transport_error_outcome_unknown', idempotency_key=key) from error


    def artifact(self, path, *, size, sha256):
        """Read exact archived bytes; redirects and implicit proxies stay disabled."""
        if (not re.fullmatch(r'/v1/world-runs/cw_[a-f0-9]{32}/evidence/(?:[0-9]|1[0-5])/(?:report|snapshot|evidence|trace)', path)
                or type(size) is not int or not 1 <= size <= 16 * 1024 * 1024
                or not isinstance(sha256, str) or not re.fullmatch(r'[a-f0-9]{64}', sha256)):
            raise ValueError('invalid artifact identity')
        req = Request(self.endpoint + path, method='GET',
                      headers={'Authorization': 'Bearer ' + self.api_key, 'Accept': 'application/json'})
        try:
            with self.opener.open(req, timeout=self.timeout) as response:
                if response.status != 200:
                    raise SDKError('http_error', status=response.status)
                raw = response.read(size + 1)
                if len(raw) != size or hashlib.sha256(raw).hexdigest() != sha256:
                    raise SDKError('artifact_integrity_error')
                return raw
        except HTTPError as error:
            error.close()
            raise SDKError('http_error', status=error.code) from None
        except (OSError, URLError, ValueError):
            raise SDKError('artifact_download_error') from None


class App:
    """World-scoped agent capability; cannot set up, observe, reset, or delete."""
    def __init__(self, endpoint, world_id, token, *, ca_file=None, timeout=200):
        self._transport = Transport(endpoint, token, ca_file=ca_file, timeout=timeout)
        self.world_id = world_id

    def operations(self):
        return self._transport.request('GET', f'/v1/worlds/{self.world_id}/operations')

    def connection(self):
        """Private connection manifest for an independently controlled harness.

        Contains a leased world capability. Do not place it in logs or model
        messages. URL origins come from this client's configured endpoint.
        """
        value = self._transport.request('GET', f'/v1/worlds/{self.world_id}/connection')
        if (not isinstance(value, dict) or value.get('schema_version') != 1
                or value.get('world_id') != self.world_id):
            raise SDKError('invalid_connection_manifest', world_id=self.world_id)
        entries = [('mcp', 'mcp'), ('app', 'call')]
        if 'environment' in value:
            entries.append(('environment', 'exec'))
        for name, suffix in entries:
            entry = value.get(name)
            path = f'/v1/worlds/{self.world_id}/{suffix}'
            if (not isinstance(entry, dict) or entry.get('path') != path
                    or entry.get('headers') != {'Authorization': 'Bearer ' + self._transport.api_key}):
                raise SDKError('invalid_connection_manifest', world_id=self.world_id)
            entry['url'] = self._transport.endpoint + entry.pop('path')
        value['ca_file'] = self._transport.ca_file
        return value

    @property
    def mcp(self):
        """Standard HTTP MCP URL and authorization headers; capability is private."""
        value = self.connection()['mcp']
        return {'url': value['url'], 'headers': value['headers']}

    def call(self, operation, /, **arguments):
        return self._transport.request('POST', f'/v1/worlds/{self.world_id}/call', {'operation': operation, 'arguments': arguments})

    def exec(self, argv, *, cwd='/home/model', timeout_seconds=30):
        """Run an argv in an explicitly enabled isolated workstation; no retries."""
        return self._transport.request('POST', f'/v1/worlds/{self.world_id}/exec',
                                       {'argv': argv, 'cwd': cwd, 'timeout_seconds': timeout_seconds})


class World:
    def __init__(self, client, value):
        self._client = client
        self.id = value['id']
        self._update(value)

    def _update(self, value):
        self.info = {key: item for key, item in value.items() if key != 'agent_token'}
        t = self._client._transport
        if value.get('agent_token'):
            self.app = App(t.endpoint, self.id, value['agent_token'], ca_file=t.ca_file, timeout=t.timeout)

    def status(self):
        value = self._client._transport.request('GET', f'/v1/worlds/{self.id}')
        self._update(value)
        return self.info

    def connection(self):
        return self.app.connection()

    def exec(self, argv, *, cwd='/home/model', timeout_seconds=30):
        return self.app.exec(argv, cwd=cwd, timeout_seconds=timeout_seconds)

    def trace(self):
        """Owner-only accepted app-call evidence, available after world cleanup."""
        return self._client._transport.request('GET', f'/v1/worlds/{self.id}/trace')

    @property
    def mcp(self):
        return self.app.mcp

    def wait(self, *, timeout=330, target='ready'):
        deadline = time.monotonic() + timeout
        while True:
            state = self.status()['state']
            if state == target:
                return self
            if state in {'failed', 'closed', 'cleanup_failed'}:
                raise SDKError(self.info.get('error') or 'world_' + state, world_id=self.id)
            if time.monotonic() >= deadline:
                raise SDKError('world_wait_timeout', world_id=self.id)
            time.sleep(0.25)

    def setup(self, *, identity, alias='primary'):
        return self._client._transport.request('POST', f'/v1/worlds/{self.id}/setup', {'identity': identity, 'alias': alias})

    def seed(self, operation, /, *, alias='primary', **arguments):
        return self._client._transport.request('POST', f'/v1/worlds/{self.id}/fixtures', {'operation': operation, 'arguments': arguments, 'alias': alias})

    def snapshot(self):
        return self._client._transport.request('GET', f'/v1/worlds/{self.id}/snapshot')

    def evidence(self):
        return self._client._transport.request('GET', f'/v1/worlds/{self.id}/evidence')

    @property
    def task(self):
        """Fetch the pinned scenario's task for the current generation."""
        value = self._client._transport.request('GET', f'/v1/worlds/{self.id}/task')
        if not isinstance(value.get('task'), str):
            raise SDKError('invalid_response', world_id=self.id)
        return value['task']

    def evaluate(self):
        """Return the independent report, including non-passing outcomes."""
        return self._client._transport.request('POST', f'/v1/worlds/{self.id}/evaluate', {})

    def start_harness(self, *, timeout_seconds=300, idempotency_key=None):
        """Start the world’s pinned customer command once; ambiguous starts are not retried."""
        if type(timeout_seconds) is not int or not 1 <= timeout_seconds <= 1200:
            raise ValueError('harness timeout must be 1–1200 seconds')
        key = idempotency_key or uuid.uuid4().hex
        return self._client._transport.request('POST', f'/v1/worlds/{self.id}/harness',
            {'timeout_seconds': timeout_seconds}, key=key)

    def harness_status(self):
        return self._client._transport.request('GET', f'/v1/worlds/{self.id}/harness')

    def reset(self, *, idempotency_key=None, timeout=330):
        key = idempotency_key or uuid.uuid4().hex
        value = self._client._transport.request('POST', f'/v1/worlds/{self.id}/reset', {}, key=key)
        self._update(value)
        return self.wait(timeout=timeout)

    def close(self, *, timeout=60):
        self._client._transport.request('DELETE', f'/v1/worlds/{self.id}')
        return self.wait(timeout=timeout, target='closed')

    def __enter__(self):
        return self

    def __exit__(self, kind, error, traceback):
        try:
            self.close()
        except Exception as cleanup:
            if error is None:
                raise
            error.add_note(f'World cleanup failed ({type(cleanup).__name__}); lease cleanup remains active for {self.id}.')


class Worlds:
    def __init__(self, client):
        self._client = client

    def create(self, *, image=None, template=None, fixture=None, mapping=None, ttl_seconds=900, idempotency_key=None, timeout=330, environment=False, harness=None):
        if (image is None) == (template is None):
            raise ValueError('provide exactly one of image or template')
        if fixture is not None and template is None:
            raise ValueError('fixture requires a template')
        for value in (template, fixture):
            if value is not None and (not isinstance(value, str) or not re.fullmatch(r'[a-z][a-z0-9-]{0,47}@[1-9][0-9]{0,8}', value)):
                raise ValueError('template and fixture require versioned references')
        key = idempotency_key or uuid.uuid4().hex
        payload = {'ttl_seconds': ttl_seconds}
        if type(environment) is not bool:
            raise ValueError('environment must be boolean')
        if environment:
            payload['environment'] = True
        if harness is not None:
            if not isinstance(harness,str) or not re.fullmatch('[a-z][a-z0-9-]{0,47}',harness): raise ValueError('invalid harness')
            payload.update(harness=harness, environment=True)
        payload['image' if image is not None else 'template'] = image if image is not None else template
        if fixture is not None:
            payload['fixture'] = fixture
        if mapping is not None:
            payload['mapping'] = mapping
        try:
            value = self._client._transport.request('POST', '/v1/worlds', payload, key=key)
            return World(self._client, value).wait(timeout=timeout)
        except SDKError as error:
            error.idempotency_key = key
            raise

    def get(self, world_id):
        return World(self._client, self._client._transport.request('GET', f'/v1/worlds/{world_id}'))


class Scenarios:
    """Operator-only generation, validation-job discovery, and suite freezing."""
    def __init__(self, client):
        self._client = client

    @staticmethod
    def _job_id(job_id):
        if not isinstance(job_id, str) or not re.fullmatch(r'[a-f0-9]{32}', job_id):
            raise ValueError('invalid scenario job ID')
        return job_id

    def status(self, job_id):
        job_id = self._job_id(job_id)
        try:
            return self._client._transport.request('GET', f'/v1/scenario-jobs/{job_id}')
        except SDKError as error:
            error.job_id = job_id
            raise

    def freeze(self, job_id, suite):
        job_id = self._job_id(job_id)
        if not isinstance(suite, str) or not re.fullmatch(r'[a-z][a-z0-9-]{0,47}@[1-9][0-9]{0,8}', suite):
            raise ValueError('suite requires a versioned reference')
        try:
            return self._client._transport.request('POST', f'/v1/scenario-jobs/{job_id}/freeze', {'suite': suite})
        except SDKError as error:
            error.job_id = job_id
            raise

    def generate(self, family, template, seed, *, count=2, distractors=2, idempotency_key=None, timeout=330):
        if family not in ('archive-note', 'close-ticket'):
            raise ValueError('unsupported scenario family')
        if not isinstance(template, str) or not re.fullmatch(r'[a-z][a-z0-9-]{0,47}@[1-9][0-9]{0,8}', template):
            raise ValueError('template requires a versioned reference')
        if type(seed) is not int or not 0 <= seed <= 2147483647:
            raise ValueError('seed must be an integer from 0 to 2147483647')
        if any(type(value) is not int or not 1 <= value <= 4 for value in (count, distractors)):
            raise ValueError('count and distractors must be integers from 1 to 4')
        if isinstance(timeout, bool) or not isinstance(timeout, (int, float)) or not math.isfinite(timeout) or timeout <= 0:
            raise ValueError('timeout must be positive and finite')
        key = uuid.uuid4().hex if idempotency_key is None else idempotency_key
        if not isinstance(key, str) or not re.fullmatch(r'[A-Za-z0-9_-]{8,128}', key):
            raise ValueError('invalid idempotency key')
        payload = {'family': family, 'template': template, 'seed': seed, 'count': count, 'distractors': distractors}
        deadline, job_id = time.monotonic() + timeout, None
        try:
            value = self._client._transport.request('POST', '/v1/scenario-jobs', payload, key=key, timeout=timeout)
            if not isinstance(value, dict) or not isinstance(value.get('id'), str) or not re.fullmatch(r'[a-f0-9]{32}', value['id']):
                raise SDKError('invalid_response')
            job_id = value['id']
            while True:
                if not isinstance(value, dict) or value.get('id') != job_id:
                    raise SDKError('invalid_response')
                state = value.get('state')
                if state == 'validated':
                    return value
                if state == 'failed':
                    raise SDKError('scenario_job_failed')
                if state not in ('queued', 'running'):
                    raise SDKError('invalid_response')
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise SDKError('scenario_wait_timeout')
                time.sleep(min(0.25, remaining))
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise SDKError('scenario_wait_timeout')
                value = self._client._transport.request('GET', f'/v1/scenario-jobs/{job_id}', timeout=remaining)
        except SDKError as error:
            error.job_id, error.idempotency_key = job_id, key
            raise


class AuthoringRole:
    """One job's writer, test-writer, or auditor credential; no owner methods."""
    def __init__(self, transport, job_id, value):
        token = value.get('token') if isinstance(value, dict) else None
        if not isinstance(token, str) or not re.fullmatch(r'[A-Za-z0-9_-]{32,128}', token):
            raise SDKError('invalid_response', job_id=job_id)
        self.job_id = job_id
        self.info = {key: value.get(key) for key in ('id', 'role', 'provider', 'model', 'session_id')}
        self._transport = Transport(transport.endpoint, token, ca_file=transport.ca_file, timeout=transport.timeout)

    def context(self):
        try:
            return self._transport.request('GET', f'/v1/authoring-jobs/{self.job_id}/context')
        except SDKError as error:
            error.job_id = self.job_id
            raise

    def submit(self, artifact, basis_sha256):
        if not isinstance(artifact, dict):
            raise ValueError('artifact must be a JSON object')
        if not isinstance(basis_sha256, str) or not re.fullmatch(r'[a-f0-9]{64}', basis_sha256):
            raise ValueError('basis must be a SHA-256 digest')
        try:
            return self._transport.request('POST', f'/v1/authoring-jobs/{self.job_id}/artifacts',
                                           {'basis_sha256': basis_sha256, 'artifact': artifact})
        except SDKError as error:
            error.job_id = self.job_id
            raise


class Authoring:
    """Trusted job management; artifact submissions belong to role handles."""
    def __init__(self, client):
        self._client = client

    @staticmethod
    def _id(job_id):
        if not isinstance(job_id, str) or not re.fullmatch(r'[a-f0-9]{32}', job_id):
            raise ValueError('invalid authoring job ID')
        return job_id

    def _request(self, method, job_id, suffix='', payload=None, *, timeout=None):
        job_id = self._id(job_id)
        try:
            return self._client._transport.request(method, f'/v1/authoring-jobs/{job_id}{suffix}', payload, timeout=timeout)
        except SDKError as error:
            error.job_id = job_id
            raise

    def create(self, brief, idempotency_key=None):
        if not isinstance(brief, dict):
            raise ValueError('brief must be a JSON object')
        key = uuid.uuid4().hex if idempotency_key is None else idempotency_key
        if not isinstance(key, str) or not re.fullmatch(r'[A-Za-z0-9_-]{8,128}', key):
            raise ValueError('invalid idempotency key')
        try:
            value = self._client._transport.request('POST', '/v1/authoring-jobs', brief, key=key)
            if not isinstance(value, dict) or not isinstance(value.get('id'), str) or not re.fullmatch(r'[a-f0-9]{32}', value['id']):
                raise SDKError('invalid_response')
            return value
        except SDKError as error:
            error.idempotency_key = key
            raise

    def status(self, job_id):
        return self._request('GET', job_id)

    def context(self, job_id):
        return self._request('GET', job_id, '/context')

    def assign(self, job_id, role, provider, model, session_id):
        self._id(job_id)
        if role not in ('writer', 'test-writer', 'auditor'):
            raise ValueError('invalid authoring role')
        if any(not isinstance(value, str) or not value.strip() for value in (provider, model, session_id)):
            raise ValueError('provider, model, and session ID are required')
        value = self._request('POST', job_id, '/roles',
                              {'role': role, 'provider': provider, 'model': model, 'session_id': session_id})
        return AuthoringRole(self._client._transport, job_id, value)

    def freeze(self, job_id, suite):
        if not isinstance(suite, str) or not re.fullmatch(r'[a-z][a-z0-9-]{0,47}@[1-9][0-9]{0,8}', suite):
            raise ValueError('suite requires a versioned reference')
        return self._request('POST', job_id, '/freeze', {'suite': suite})

    def validate(self, job_id, timeout=330):
        return self._wait(job_id, 'validate', timeout)

    def run(self, job_id, timeout=900):
        return self._wait(job_id, 'run', timeout)

    def live(self, job_id, timeout=900):
        return self._wait(job_id, 'live', timeout)

    def _wait(self, job_id, action, timeout):
        self._id(job_id)
        if isinstance(timeout, bool) or not isinstance(timeout, (int, float)) or not math.isfinite(timeout) or timeout <= 0:
            raise ValueError('timeout must be positive and finite')
        deadline = time.monotonic() + timeout
        value = self._request('POST', job_id, '/'+action, {}, timeout=timeout)
        while True:
            if not isinstance(value, dict) or value.get('id') != job_id:
                raise SDKError('invalid_response', job_id=job_id)
            state = value.get('state')
            if state == 'accepted' or (action == 'validate' and state in ('awaiting_audit', 'ready_for_live')):
                return value
            if state in ('rejected', 'validation_failed', 'live_failed'):
                raise SDKError('authoring_'+state, job_id=job_id)
            runner = value.get('runner')
            if action in ('run', 'live') and isinstance(runner, dict) and runner.get('state') == 'failed':
                raise SDKError('authoring_run_failed', job_id=job_id)
            if state not in ('awaiting_seed', 'awaiting_tests', 'ready_for_validation', 'validating', 'awaiting_audit', 'ready_for_live', 'live_evaluating'):
                raise SDKError('invalid_response', job_id=job_id)
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise SDKError('authoring_wait_timeout', job_id=job_id)
            time.sleep(min(.25, remaining))
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise SDKError('authoring_wait_timeout', job_id=job_id)
            value = self._request('GET', job_id, timeout=remaining)


class Apps:
    """Owner-only onboarding of immutable images already staged on the worker.

    This client never runs Docker, builds images, uploads files, or pulls images.
    """
    def __init__(self, client):
        self._client = client

    @staticmethod
    def _id(job_id):
        if not isinstance(job_id, str) or not re.fullmatch(r'[a-f0-9]{32}', job_id):
            raise ValueError('invalid app import job ID')
        return job_id

    @staticmethod
    def _response(value, job_id):
        if not isinstance(value, dict) or value.get('id') != job_id or value.get('state') not in ('queued', 'importing', 'validating', 'ready', 'failed'):
            raise SDKError('invalid_response', job_id=job_id)
        return value

    def validate(self, manifest):
        """Check manifest structure only; no image execution or publication."""
        if not isinstance(manifest, dict):
            raise ValueError('manifest must be a JSON object')
        value = self._client._transport.request('POST', '/v1/app-contracts/validate', manifest)
        if not isinstance(value, dict) or value.get('valid') is not True:
            raise SDKError('invalid_response')
        return value

    def status(self, job_id):
        job_id = self._id(job_id)
        try:
            value = self._client._transport.request('GET', f'/v1/app-import-jobs/{job_id}')
            return self._response(value, job_id)
        except SDKError as error:
            error.job_id = job_id
            raise

    def import_app(self, manifest, idempotency_key=None, timeout=1200):
        """Execute the supplied contract, wait for readiness, and return its receipt.

        Failed/uncertain calls retain job_id and idempotency_key on SDKError.
        Writes are never retried automatically. Explicit same-key replay is safe.
        """
        if not isinstance(manifest, dict):
            raise ValueError('manifest must be a JSON object')
        if isinstance(timeout, bool) or not isinstance(timeout, (int, float)) or not math.isfinite(timeout) or timeout <= 0:
            raise ValueError('timeout must be positive and finite')
        key = uuid.uuid4().hex if idempotency_key is None else idempotency_key
        if not isinstance(key, str) or not re.fullmatch(r'[A-Za-z0-9_-]{8,128}', key):
            raise ValueError('invalid idempotency key')
        deadline, job_id = time.monotonic() + timeout, None
        try:
            value = self._client._transport.request('POST', '/v1/app-import-jobs', manifest, key=key, timeout=timeout)
            if not isinstance(value, dict) or not isinstance(value.get('id'), str) or not re.fullmatch(r'[a-f0-9]{32}', value['id']):
                raise SDKError('invalid_response')
            job_id = value['id']
            while True:
                self._response(value, job_id)
                state = value['state']
                if state == 'ready':
                    return dict(value, idempotency_key=key)
                if state == 'failed':
                    raise SDKError('app_import_failed')
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise SDKError('app_import_wait_timeout')
                time.sleep(min(.25, remaining))
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise SDKError('app_import_wait_timeout')
                value = self._client._transport.request('GET', f'/v1/app-import-jobs/{job_id}', timeout=remaining)
        except SDKError as error:
            error.job_id, error.idempotency_key = job_id, key
            raise


class SuiteCase(NamedTuple):
    """Public agent input. Contains no owner handle or private grading data."""
    task: str
    app: App
    case_ref: str


class Suites:
    """Trusted suite orchestration with one fresh, leased world per case."""
    _states = {'pending', 'running', 'completed', 'cancelling', 'cancelled', 'failed'}
    _case_states = {'pending', 'running', 'finishing', 'passed', 'failed', 'error',
                    'inconclusive', 'invalid', 'not_run'}

    def __init__(self, client):
        self._client = client

    @staticmethod
    def _id(run_id):
        if not isinstance(run_id, str) or not re.fullmatch(r'[a-f0-9]{32}', run_id):
            raise ValueError('invalid suite run ID')
        return run_id

    @staticmethod
    def _claim(claim):
        if not isinstance(claim, str) or not re.fullmatch(r'[A-Za-z0-9_-]{32,128}', claim):
            raise ValueError('invalid suite case claim')

    @classmethod
    def _response(cls, value, run_id=None):
        if (not isinstance(value, dict) or not isinstance(value.get('id'), str)
                or not re.fullmatch(r'[a-f0-9]{32}', value['id'])
                or (run_id is not None and value['id'] != run_id)
                or value.get('state') not in cls._states
                or not isinstance(value.get('cases'), list)):
            raise SDKError('invalid_response', run_id=run_id)
        for index, case in enumerate(value['cases']):
            if (not isinstance(case, dict) or case.get('index') != index
                    or case.get('state') not in cls._case_states):
                raise SDKError('invalid_response', run_id=value['id'])
        return value

    def _request(self, method, run_id, suffix='', payload=None, *, timeout=None):
        self._id(run_id)
        try:
            return self._client._transport.request(method, f'/v1/suite-runs/{run_id}{suffix}', payload, timeout=timeout)
        except SDKError as error:
            error.run_id = run_id
            raise

    def create(self, suite, revision, *, ttl_seconds=1800, idempotency_key=None, timeout=None, environment=False, harness=None):
        if type(environment) is not bool:
            raise ValueError('environment must be boolean')
        if not isinstance(suite, str) or not re.fullmatch(r'[a-z][a-z0-9-]{0,47}@[1-9][0-9]{0,8}', suite):
            raise ValueError('suite requires a versioned reference')
        if not isinstance(revision, str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._/@:+-]{0,127}', revision):
            raise ValueError('invalid revision label')
        if type(ttl_seconds) is not int or not 60 <= ttl_seconds <= 3600:
            raise ValueError('ttl_seconds must be an integer from 60 to 3600')
        if harness is not None and (not isinstance(harness,str) or not re.fullmatch('[a-z][a-z0-9-]{0,47}',harness)):
            raise ValueError('invalid harness')
        key = uuid.uuid4().hex if idempotency_key is None else idempotency_key
        if not isinstance(key, str) or not re.fullmatch(r'[A-Za-z0-9_-]{8,128}', key):
            raise ValueError('invalid idempotency key')
        try:
            return self._response(self._client._transport.request('POST', '/v1/suite-runs',
                {'suite': suite, 'revision': revision, 'ttl_seconds': ttl_seconds, **({'environment': True} if environment or harness else {}), **({'harness':harness} if harness else {})}, key=key, timeout=timeout))
        except SDKError as error:
            error.idempotency_key = key
            raise

    def status(self, run_id):
        return self._response(self._request('GET', run_id), run_id)

    def next(self, run_id, claim, *, timeout=None):
        """Claim the next case. The returned world contains a private capability.

        Explicit same-claim replay recovers an assignment; never execute an agent
        again just because a response was lost. A different claim cannot steal it.
        """
        self._claim(claim)
        value = self._request('POST', run_id, '/next', {'claim': claim}, timeout=timeout)
        if not isinstance(value, dict) or 'case' not in value:
            raise SDKError('invalid_response', run_id=run_id)
        self._response(value.get('run'), run_id)
        case = value['case']
        if case is not None:
            if (not isinstance(case, dict) or type(case.get('index')) is not int
                    or not 0 <= case['index'] < len(value['run']['cases'])
                    or not isinstance(case.get('ref'), str)
                    or not isinstance(case.get('world'), dict)):
                raise SDKError('invalid_response', run_id=run_id)
        return value

    def finish(self, run_id, case_index, claim, agent_status='completed', *, timeout=None):
        self._claim(claim)
        if type(case_index) is not int or case_index < 0:
            raise ValueError('invalid suite case index')
        if agent_status not in ('completed', 'error', 'timeout'):
            raise ValueError('invalid agent status')
        return self._response(self._request('POST', run_id, '/finish',
            {'case_index': case_index, 'claim': claim, 'agent_status': agent_status}, timeout=timeout), run_id)

    def cancel(self, run_id, *, timeout=None):
        return self._response(self._request('POST', run_id, '/cancel', {}, timeout=timeout), run_id)

    def compare(self, baseline, candidate):
        self._id(baseline)
        self._id(candidate)
        value = self._client._transport.request('POST', '/v1/suite-run-comparisons',
                                                {'baseline': baseline, 'candidate': candidate})
        if not isinstance(value, dict):
            raise SDKError('invalid_response')
        return value

    def run_harness(self, suite, harness, revision, **kwargs):
        """Run an operator-registered image/command inside every case workstation."""
        return self.run(suite, None, revision, harness=harness, **kwargs)

    def run(self, suite, agent, revision, *, idempotency_key=None, ttl_seconds=1800, timeout=1800, environment=False, harness=None):
        """Run a synchronous callback sequentially against fresh suite cases.

        The callback must bound its own execution; this method cannot interrupt
        Python code. Worker leases independently revoke expired world access.
        Callback return values are ignored. Ordinary exceptions record an agent
        error and continue, while infrastructure/transport errors stop execution.
        """
        if (harness is None and not callable(agent)) or (harness is not None and agent is not None):
            raise ValueError('supply either a callback or a harness alias')
        if isinstance(timeout, bool) or not isinstance(timeout, (int, float)) or not math.isfinite(timeout) or timeout <= 0:
            raise ValueError('timeout must be positive and finite')
        deadline = time.monotonic() + timeout
        key = uuid.uuid4().hex if idempotency_key is None else idempotency_key
        run_id, claim, may_cancel = None, secrets.token_urlsafe(32), False

        def remaining():
            result = deadline - time.monotonic()
            if result <= 0:
                raise SDKError('suite_run_timeout', run_id=run_id)
            return result

        def poll():
            time.sleep(min(.25, remaining()))
            return self._response(self._request('GET', run_id, timeout=remaining()), run_id)

        def terminal(value):
            if value['state'] in ('failed', 'cancelled'):
                raise SDKError('suite_run_' + value['state'], run_id=run_id)
            return value['state'] == 'completed'

        try:
            value = self.create(suite, revision, ttl_seconds=ttl_seconds, idempotency_key=key, timeout=remaining(), environment=environment, harness=harness)
            run_id = value['id']
            if terminal(value):
                return value
            # A fresh invocation cannot establish whether an earlier agent ran.
            if value['state'] != 'pending' or any(c['state'] != 'pending' for c in value['cases']):
                raise SDKError('suite_run_resume_requires_explicit_control', run_id=run_id)
            may_cancel = True
            while True:
                assignment = self.next(run_id, claim, timeout=remaining())
                value, case = assignment['run'], assignment['case']
                if terminal(value):
                    return value
                if case is None:
                    raise SDKError('invalid_response', run_id=run_id)
                world = case['world']
                world_id = world.get('id')
                if not isinstance(world_id, str) or not re.fullmatch(r'[a-f0-9]{32}', world_id):
                    raise SDKError('invalid_response', run_id=run_id)
                token = world.get('agent_token')
                while world.get('state') not in ('ready', 'failed', 'closed', 'cleanup_failed'):
                    if world.get('state') not in ('creating', 'resetting', 'deleting'):
                        raise SDKError('invalid_response', run_id=run_id, world_id=world_id)
                    time.sleep(min(.25, remaining()))
                    world = self._client._transport.request('GET', f'/v1/worlds/{world_id}', timeout=remaining())
                    if not isinstance(world, dict) or world.get('id') != world_id:
                        raise SDKError('invalid_response', run_id=run_id, world_id=world_id)
                    token = world.get('agent_token') or token
                agent_status = 'error'
                if world['state'] == 'ready':
                    if not isinstance(token, str) or not re.fullmatch(r'[A-Za-z0-9_-]{32,128}', token):
                        raise SDKError('invalid_response', run_id=run_id, world_id=world_id)
                    t = self._client._transport
                    app = App(t.endpoint, world_id, token, ca_file=t.ca_file, timeout=min(t.timeout, remaining()))
                    task = app._transport.request('GET', f'/v1/worlds/{world_id}/task', timeout=remaining())
                    if not isinstance(task, dict) or not isinstance(task.get('task'), str):
                        raise SDKError('invalid_response', run_id=run_id, world_id=world_id)
                    try:
                        if harness is None:
                            agent(SuiteCase(task['task'], app, case['ref']))
                            agent_status = 'completed'
                        else:
                            path = f'/v1/worlds/{world_id}/harness'
                            job = t.request('POST',path,{'timeout_seconds':min(1200,max(1,int(remaining()-30)))},key=claim,timeout=remaining())
                            while job['state'] in ('starting','running'):
                                time.sleep(min(.25,remaining()))
                                job=t.request('GET',path,timeout=remaining())
                            agent_status='completed' if job['state']=='completed' and not job.get('error') else 'timeout' if job['state']=='timeout' else 'error'
                    except Exception:
                        # Customer exception messages can include secrets; never send them.
                        agent_status = 'error'
                value = self.finish(run_id, case['index'], claim, agent_status, timeout=remaining())
                while not terminal(value) and value['cases'][case['index']]['state'] in ('running', 'finishing'):
                    value = poll()
                if value['cases'][case['index']].get('cleanup_confirmed') is not True:
                    raise SDKError('suite_case_cleanup_unconfirmed', run_id=run_id)
                if terminal(value):
                    return value
        except BaseException as error:
            if isinstance(error, SDKError):
                error.run_id, error.idempotency_key = run_id, key
                if error.code == 'case_already_claimed':
                    may_cancel = False
            if may_cancel and run_id is not None:
                try:
                    self.cancel(run_id, timeout=5)
                except Exception:
                    error.add_note('Suite cancellation was not confirmed; inspect run status. Worker leases remain active.')
            raise


class Client:
    def __init__(self, endpoint, api_key, *, ca_file=None, timeout=200):
        self._transport = Transport(endpoint, api_key, ca_file=ca_file, timeout=timeout)
        self.apps = Apps(self)
        self.worlds = Worlds(self)
        self.scenarios = Scenarios(self)
        self.authoring = Authoring(self)
        self.suites = Suites(self)

    def catalog(self):
        return self._transport.request('GET', '/v1/catalog')
