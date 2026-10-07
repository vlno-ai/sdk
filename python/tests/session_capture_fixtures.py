"""Copy-on-write sink store double, optionally using actual durable file writes."""
import copy
import json
from pathlib import Path
from datetime import datetime
from types import SimpleNamespace
from unittest.mock import Mock, patch

from closed_world_sdk.platform.agent import PlatformCase

from vlno.sessions.json import encode
from vlno.sessions.schema import validate
from vlno.sessions.capture import DurableRecorder

KEY, TOKEN, CLAIM = 'controller-secret-123', 'scoped-secret-' + 'B' * 32, 'A' * 43


class SinkStore:
    def __init__(self, files=None):
        root = Path(__file__).resolve().parents[2]
        self._document = json.loads((root / 'testdata/session/confirmed.json').read_text())
        self.claim = CLAIM
        self._document['claim'].update(state='ready', actor=self._document['admission']['actor'],
            case={'index': 0, 'scenario': 'archive-note@1', 'worldId': 'b' * 32,
                  'generation': 1, 'expiresAt': '2030-10-07T09:02:00.000Z'})
        self.files, self.failed = files, False
        self.calls, self.fail_at = 0, None
        if files:
            files.write_new('journal.json', encode(self._document))

    @property
    def document(self):
        return copy.deepcopy(self._document)

    def update(self, mutator):
        self.calls += 1
        if self.failed or self.calls == self.fail_at:
            raise OSError('synthetic fsync failure')
        candidate = self.document
        mutator(candidate)
        candidate['revision'] += 1
        validate(candidate)
        encoded = encode(candidate)
        if self.files:
            try:
                self.files.replace('journal.json', encoded)
            except Exception:
                self.failed = True
                raise
        self._document = copy.deepcopy(candidate)


def rig(store=None):
    store = store or SinkStore()
    run = SimpleNamespace(id=store.document['admission']['receipt']['runId'],
                          _client=SimpleNamespace(_transport=SimpleNamespace(api_key=KEY,
                              endpoint='https://api.example.test', ca_file=None, timeout=30)))
    saved = store.document['claim']['case']
    world, task = saved['worldId'], 'Print an ordinary greeting.'
    expires = datetime.fromisoformat(saved['expiresAt']).timestamp()
    headers = {'Authorization': 'Bearer ' + TOKEN}
    manifest = {'schema_version': 1, 'generation': saved['generation'], 'world_id': world,
                'task': task, 'expires_at': expires,
                'mcp': {'path': f'/v1/world-agent/{world}/mcp', 'headers': headers,
                        'transport': 'streamable-http'},
                'app': {'path': f'/v1/world-agent/{world}/call', 'headers': headers}}
    with patch('closed_world_sdk.platform.agent.time.time', return_value=expires - 1):
        case = PlatformCase(run._client, run.id, {'index': saved['index'], 'scenario': saved['scenario'],
            'worldId': world, 'task': task, 'expiresAt': expires, 'connection': manifest})
    sent = []
    def upload(method, path, body):
        sent.append(copy.deepcopy(body))
        return {'accepted': [body['events'][0]['id']]}
    run._request = Mock(side_effect=upload)
    return DurableRecorder(run, case, CLAIM, store), store, sent
