"""App onboarding transport tests; no Docker or application code executes."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('generation_tests', Path(__file__).with_name('test_generation.py'))
base = importlib.util.module_from_spec(spec)
spec.loader.exec_module(base)
sdk, JOB = base.sdk, base.JOB
MANIFEST = {'name': 'notes', 'template': 'notes@1', 'description': 'Notes workflow',
            'source_image': 'sha256:' + 'a' * 64, 'mapping': {}, 'contract': {}}


class AppsTest(unittest.TestCase):
    setUp = base.GenerationTest.setUp
    tearDown = base.GenerationTest.tearDown
    def import_app(self, **options):
        return self.client.apps.import_app(MANIFEST, **dict({'idempotency_key': 'app-import-replay'}, **options))

    def test_import_states_validate_and_status(self):
        states = iter(('queued', 'importing', 'validating', 'ready', 'ready'))
        def reply(method, path, payload):
            if path == '/v1/app-contracts/validate':
                return 200, {'valid': True, 'name': 'notes', 'template': 'notes@1', 'mapping_sha256': 'a'*64, 'contract_sha256': 'b'*64, 'limitations': []}
            return 200, {'id': JOB, 'state': next(states)}
        self.reply = reply
        self.assertTrue(self.client.apps.validate(MANIFEST)['valid'])
        result = self.import_app()
        self.assertEqual(result['state'], 'ready')
        self.assertEqual(result['idempotency_key'], 'app-import-replay')
        self.assertEqual(self.client.apps.status(JOB)['id'], JOB)
        validation, create, *polls = self.requests
        self.assertEqual(validation[:3], ('POST', '/v1/app-contracts/validate', MANIFEST))
        self.assertNotIn('Idempotency-Key', validation[3])
        self.assertEqual(create[:3], ('POST', '/v1/app-import-jobs', MANIFEST))
        self.assertEqual(create[3]['Idempotency-Key'], 'app-import-replay')
        self.assertTrue(all(r[:2] == ('GET', f'/v1/app-import-jobs/{JOB}') for r in polls))
        self.assertTrue(all(r[3]['Authorization'] == 'Bearer '+base.KEY for r in self.requests))

    def test_import_failure_timeout_unknown_states(self):
        for state, code in [('failed', 'app_import_failed'), ('validating', 'app_import_wait_timeout'), ('unexpected', 'invalid_response')]:
            with self.subTest(state=state):
                self.reply = lambda *_: (200, {'id': JOB, 'state': state, 'error': 'private arbitrary text'})
                with self.assertRaises(sdk.SDKError) as caught:
                    self.import_app(timeout=.01)
                self.assertEqual(caught.exception.code, code)
                self.assertEqual(caught.exception.job_id, JOB)
                self.assertEqual(caught.exception.idempotency_key, 'app-import-replay')
                self.assertNotIn('private arbitrary', str(caught.exception))

    def test_import_lost_response_and_conflict_are_not_retried(self):
        for status, expected in [(None, 'transport_error_outcome_unknown'), (409, 'idempotency_conflict')]:
            with self.subTest(status=status):
                self.requests.clear()
                self.reply = lambda *_: (status, {'error': {'code': 'idempotency_conflict'}})
                with self.assertRaises(sdk.SDKError) as caught:
                    self.import_app()
                self.assertEqual(caught.exception.code, expected)
                self.assertEqual(caught.exception.idempotency_key, 'app-import-replay')
                self.assertIsNone(caught.exception.job_id)
                self.assertEqual(len(self.requests), 1)

    def test_import_poll_authorization_error_keeps_metadata(self):
        self.reply = lambda method, *_: (200, {'id': JOB, 'state': 'queued'}) if method == 'POST' else (401, {'error': {'code': 'unauthorized'}})
        with self.assertRaises(sdk.SDKError) as caught:
            self.import_app()
        self.assertEqual(caught.exception.status, 401)
        self.assertEqual(caught.exception.job_id, JOB)
        self.assertEqual(sum(r[0] == 'POST' for r in self.requests), 1)

    def test_import_changed_identity_and_invalid_validation(self):
        self.reply = lambda method, *_: (200, {'id': JOB if method == 'POST' else 'c'*32, 'state': 'queued' if method == 'POST' else 'ready'})
        with self.assertRaises(sdk.SDKError) as caught:
            self.import_app()
        self.assertEqual(caught.exception.code, 'invalid_response')
        self.assertEqual(caught.exception.job_id, JOB)
        self.reply = lambda *_: (200, {'valid': False})
        with self.assertRaises(sdk.SDKError):
            self.client.apps.validate(MANIFEST)

    def test_app_input_errors_never_send(self):
        for options in ({'timeout': 0}, {'timeout': True}, {'timeout': float('nan')}, {'idempotency_key': ''}):
            with self.subTest(options=options), self.assertRaises(ValueError):
                self.import_app(**options)
        for action in (lambda: self.client.apps.validate([]), lambda: self.client.apps.import_app([]), lambda: self.client.apps.status('../bad')):
            with self.assertRaises(ValueError):
                action()
        self.assertEqual(self.requests, [])


if __name__ == '__main__':
    unittest.main()
