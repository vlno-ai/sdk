"""Client protocol tests using only stdlib and an ordinary local HTTP service."""
import importlib.util
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import threading
import unittest

spec = importlib.util.spec_from_file_location('generation_sdk', Path(__file__).parents[1] / 'src/closed_world_sdk/__init__.py')
sdk = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sdk)
JOB = 'b' * 32
KEY = 'a' * 32


class GenerationTest(unittest.TestCase):
    def setUp(self):
        self.requests = []
        self.reply = lambda *_: (200, {'id': JOB, 'state': 'validated'})
        owner = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def handle_api(self):
                data = self.rfile.read(int(self.headers.get('Content-Length', 0)))
                payload = json.loads(data) if data else None
                owner.requests.append((self.command, self.path, payload, dict(self.headers)))
                status, value = owner.reply(self.command, self.path, payload)
                if status is None:
                    self.close_connection = True
                    return
                raw = json.dumps(value).encode()
                self.send_response(status)
                self.send_header('Content-Length', str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)

            do_GET = do_POST = handle_api

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, kwargs={'poll_interval': .01})
        self.thread.start()
        self.client = sdk.Client(f'http://127.0.0.1:{self.server.server_port}', KEY, timeout=1)

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def generate(self, **kwargs):
        options = dict(family='archive-note', template='notes-workspace@1', seed=7, idempotency_key='generation-replay')
        options.update(kwargs)
        return self.client.scenarios.generate(**options)

    def test_generate_poll_status_freeze_contract(self):
        def reply(method, path, payload):
            if path.endswith('/freeze'):
                return 200, {'ref': payload['suite'], 'job_id': JOB, 'cases': [], 'suite_sha256': 'hash'}
            return 200, {'id': JOB, 'state': 'queued' if method == 'POST' else 'validated'}
        self.reply = reply
        self.assertEqual(self.generate()['state'], 'validated')
        self.assertEqual(self.client.scenarios.status(JOB)['id'], JOB)
        self.assertEqual(self.client.scenarios.freeze(JOB, 'notes-regression@1')['ref'], 'notes-regression@1')
        create, poll, status, freeze = self.requests
        self.assertEqual(create[:3], ('POST', '/v1/scenario-jobs', {'family': 'archive-note', 'template': 'notes-workspace@1', 'seed': 7, 'count': 2, 'distractors': 2}))
        self.assertEqual(create[3]['Idempotency-Key'], 'generation-replay')
        self.assertTrue(all(item[3]['Authorization'] == 'Bearer ' + KEY for item in self.requests))
        self.assertEqual(freeze[:3], ('POST', f'/v1/scenario-jobs/{JOB}/freeze', {'suite': 'notes-regression@1'}))
        self.assertNotIn('Idempotency-Key', freeze[3])

    def test_failed_and_timed_out_jobs_keep_recovery_metadata(self):
        for state, expected in [('failed', 'scenario_job_failed'), ('running', 'scenario_wait_timeout')]:
            with self.subTest(state=state):
                self.reply = lambda *_: (200, {'id': JOB, 'state': state, 'error': 'arbitrary private text'})
                with self.assertRaises(sdk.SDKError) as caught:
                    self.generate(timeout=.01)
                self.assertEqual(caught.exception.code, expected)
                self.assertEqual(caught.exception.job_id, JOB)
                self.assertEqual(caught.exception.idempotency_key, 'generation-replay')

    def test_lost_create_response_is_not_retried(self):
        self.reply = lambda *_: (None, None)
        with self.assertRaises(sdk.SDKError) as caught:
            self.generate()
        self.assertEqual(len(self.requests), 1)
        self.assertIsNone(caught.exception.job_id)
        self.assertEqual(caught.exception.idempotency_key, 'generation-replay')

    def test_authorization_failure_and_poll_failure(self):
        self.reply = lambda method, *_: (200, {'id': JOB, 'state': 'queued'}) if method == 'POST' else (401, {'error': {'code': 'unauthorized'}})
        with self.assertRaises(sdk.SDKError) as caught:
            self.generate()
        self.assertEqual(caught.exception.status, 401)
        self.assertEqual(caught.exception.job_id, JOB)
        self.assertEqual(caught.exception.idempotency_key, 'generation-replay')
        self.assertEqual(sum(request[0] == 'POST' for request in self.requests), 1)

    def test_invalid_inputs_never_send(self):
        for options in ({'seed': True}, {'seed': -1}, {'seed': 2147483648}, {'count': 0}, {'distractors': 5}, {'count': 1.0}, {'family': 'generate-anything'}, {'template': 'notes'}, {'idempotency_key': ''}, {'timeout': float('nan')}, {'timeout': 0}):
            with self.subTest(options=options), self.assertRaises(ValueError):
                self.generate(**options)
        with self.assertRaises(ValueError):
            self.client.scenarios.status('../outside')
        with self.assertRaises(ValueError):
            self.client.scenarios.freeze(JOB, 'unversioned')
        self.assertEqual(self.requests, [])

    def test_changed_job_identity_is_rejected(self):
        self.reply = lambda method, *_: (200, {'id': JOB if method == 'POST' else 'c' * 32, 'state': 'queued' if method == 'POST' else 'validated'})
        with self.assertRaises(sdk.SDKError) as caught:
            self.generate()
        self.assertEqual(caught.exception.code, 'invalid_response')
        self.assertEqual(caught.exception.job_id, JOB)


if __name__ == '__main__':
    unittest.main()
