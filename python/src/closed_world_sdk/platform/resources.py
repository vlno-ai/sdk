"""Discover suites and create or retrieve hosted runs."""
import copy
import re
import uuid

from .. import SDKError
from .run import PlatformRun
from .validation import _REF, _RUN, _match, _view

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


class PlatformSuites:
    """Discover exact hosted suite versions granted to the authenticated organization."""
    def __init__(self, client):
        self._client = client

    def list(self):
        value = self._client._transport.request('GET', '/v1/world-suites')
        try:
            items = value['items']
            if not isinstance(items, list) or len(items) > 128:
                raise ValueError()
            seen = set()
            for item in items:
                ref = _match(_REF, item['suite'], 'suite')
                if ref in seen or not re.fullmatch(r'[a-f0-9]{64}', item['suiteSha256']):
                    raise ValueError()
                seen.add(ref)
                for name, limit in [('title', 120), ('description', 2000)]:
                    if not isinstance(item[name], str) or not 1 <= len(item[name]) <= limit:
                        raise ValueError()
                if type(item['environmentAllowed']) is not bool or item['transports'] != ['app', 'mcp']:
                    raise ValueError()
                if (not isinstance(item['limitations'], list) or not 1 <= len(item['limitations']) <= 16
                        or any(not isinstance(s, str) or not 1 <= len(s) <= 500 for s in item['limitations'])):
                    raise ValueError()
                if (not isinstance(item['harnesses'], list) or len(item['harnesses']) > 16
                        or any(not isinstance(s, str) or not re.fullmatch(r'[a-z][a-z0-9-]{0,47}', s) for s in item['harnesses'])
                        or item['harnesses'] and not item['environmentAllowed']):
                    raise ValueError()
            return copy.deepcopy(items)
        except (KeyError, TypeError, ValueError):
            raise SDKError('invalid_suite_catalog') from None

