"""Suite runner protocol and callback-boundary tests against a local HTTP server."""
import importlib.util
from pathlib import Path
import time
import unittest

spec = importlib.util.spec_from_file_location('generation_tests', Path(__file__).with_name('test_generation.py'))
base = importlib.util.module_from_spec(spec)
spec.loader.exec_module(base)
sdk, RUN = base.sdk, base.JOB
WORLD = 'c' * 32
TOKEN = 'agent' + 'd' * 32
CLAIM = 'q' * 43


def metadata(state='pending', case_state='pending', cleanup=False):
    return {'id': RUN, 'state': state, 'cases': [
        {'index': 0, 'ref': 'archive-note@1', 'state': case_state,
         'cleanup_confirmed': cleanup, 'world_id': WORLD if case_state != 'pending' else None}]}


class SuiteProtocol:
    setUp = base.GenerationTest.setUp
    tearDown = base.GenerationTest.tearDown

    def protocol(self, *, world_state='ready', result='passed', finish_pending=True):
        self.progress = 'pending'
        self.world_polls = 0
        def reply(method, path, payload):
            if path == '/v1/suite-runs':
                return 201, metadata()
            if path.endswith('/next'):
                self.progress = 'running'
                return 200, {'run': metadata('running', 'running'), 'case': {
                    'index': 0, 'ref': 'archive-note@1', 'world': {
                        'id': WORLD, 'state': world_state, 'agent_token': TOKEN,
                        'private_seed': 'not a callback input'}}}
            if path == f'/v1/worlds/{WORLD}':
                self.world_polls += 1
                return 200, {'id': WORLD, 'state': 'ready', 'agent_token': TOKEN}
            if path.endswith('/task'):
                return 200, {'task': 'Archive the release note.', 'private_assertions': ['hidden']}
            if path.endswith('/operations'):
                return 200, {'operations': ['archive_note']}
            if path.endswith('/call'):
                return 200, {'status': 'ok'}
            if path.endswith('/finish'):
                self.progress = 'finishing'
                return 202, metadata('running', 'finishing') if finish_pending else metadata('completed', result, True)
            if path.endswith('/cancel'):
                return 202, metadata('cancelling', 'finishing')
            if path == f'/v1/suite-runs/{RUN}':
                self.progress = 'completed'
                return 200, metadata('completed', result, True)
            raise AssertionError(path)
        self.reply = reply

