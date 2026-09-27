"""Authoring operator/role separation and recovery over ordinary local HTTP."""
import unittest

import test_generation as generation

JOB, KEY, sdk = generation.JOB, generation.KEY, generation.sdk

ROLE = 'r' * 40
BASIS = 'c' * 64
BRIEF = {'template': 'notes-workspace@1', 'task': 'Archive the selected note.',
         'requirements': [{'id': 'archive', 'category': 'task', 'description': 'Archive only the selected note.'}]}


class AuthoringTest(unittest.TestCase):
    setUp = generation.GenerationTest.setUp
    tearDown = generation.GenerationTest.tearDown

    def test_create_role_context_and_submit_use_separate_credentials(self):
        def reply(method, path, payload):
            if path.endswith('/roles'):
                return 200, {'id': 'd' * 32, **payload, 'token': ROLE}
            if path.endswith('/context'):
                return 200, {'job': {'id': JOB, 'basis_sha256': BASIS}, 'app_mapping': {}}
            return 200, {'id': JOB, 'state': 'awaiting_tests' if path.endswith('/artifacts') else 'awaiting_seed'}
        self.reply = reply
        job = self.client.authoring.create(BRIEF, idempotency_key='authoring-replay')
        self.assertEqual(job['id'], JOB)
        role = self.client.authoring.assign(JOB, 'writer', 'anthropic', 'claude-sonnet-5', 'writer-session')
        self.assertNotIn('token', role.info)
        self.assertNotIn(ROLE, str(role.info))
        self.assertFalse(hasattr(self.client.authoring, 'submit'))
        self.assertFalse(hasattr(role, 'freeze'))
        self.assertEqual(role.context()['job']['basis_sha256'], BASIS)
        artifact = {'identities': {}, 'fixtures': [], 'success_state': []}
        self.assertEqual(role.submit(artifact, BASIS)['state'], 'awaiting_tests')
        self.client.authoring.context(JOB)
        self.assertEqual(self.requests[0][:3], ('POST', '/v1/authoring-jobs', BRIEF))
        self.assertEqual(self.requests[0][3]['Idempotency-Key'], 'authoring-replay')
        self.assertEqual(self.requests[1][2]['session_id'], 'writer-session')
        self.assertEqual(self.requests[2][3]['Authorization'], 'Bearer ' + ROLE)
        self.assertEqual(self.requests[3][:3], ('POST', f'/v1/authoring-jobs/{JOB}/artifacts', {'basis_sha256': BASIS, 'artifact': artifact}))
        self.assertEqual(self.requests[3][3]['Authorization'], 'Bearer ' + ROLE)
        self.assertEqual(self.requests[4][3]['Authorization'], 'Bearer ' + KEY)

    def test_run_waits_through_audits_and_honest_live_gate(self):
        states = iter(('awaiting_tests', 'ready_for_live', 'live_evaluating', 'accepted'))
        self.reply = lambda *_: (200, {'id': JOB, 'state': next(states), 'runner': {'state': 'running'}})
        self.assertEqual(self.client.authoring.run(JOB, timeout=5)['state'], 'accepted')
        self.assertEqual(self.requests[0][:3], ('POST', f'/v1/authoring-jobs/{JOB}/run', {}))
        self.assertEqual(sum(request[0] == 'POST' for request in self.requests), 1)

    def test_validate_stops_for_audit_and_explicit_live_finishes(self):
        self.reply = lambda *_: (200, {'id': JOB, 'state': 'awaiting_audit'})
        self.assertEqual(self.client.authoring.validate(JOB)['state'], 'awaiting_audit')
        self.assertEqual(len(self.requests), 1)
        self.assertEqual(self.requests[0][:3], ('POST', f'/v1/authoring-jobs/{JOB}/validate', {}))
        self.reply = lambda *_: (200, {'id': JOB, 'state': 'accepted'})
        self.assertEqual(self.client.authoring.live(JOB)['state'], 'accepted')
        self.assertEqual(self.requests[-1][:3], ('POST', f'/v1/authoring-jobs/{JOB}/live', {}))
        self.reply = lambda _, __, payload: (200, {'ref': payload['suite'], 'cases': []})
        self.assertEqual(self.client.authoring.freeze(JOB, 'authored-notes@1')['ref'], 'authored-notes@1')

    def test_failures_and_timeouts_preserve_authoring_job_id(self):
        for state in ('rejected', 'validation_failed', 'live_failed', 'validating', 'runner_failed'):
            with self.subTest(state=state):
                response = {'id': JOB, 'state': state}
                if state == 'runner_failed':
                    response.update(state='awaiting_seed', runner={'state': 'failed', 'error': 'private provider detail'})
                self.reply = lambda *_: (200, response)
                with self.assertRaises(sdk.SDKError) as caught:
                    self.client.authoring.run(JOB, timeout=.01)
                self.assertEqual(caught.exception.job_id, JOB)
                self.assertNotIn('private', str(caught.exception))
        self.reply = lambda *_: (503, {'error': {'code': 'model_not_configured'}})
        with self.assertRaises(sdk.SDKError) as caught:
            self.client.authoring.run(JOB)
        self.assertEqual(caught.exception.job_id, JOB)
        self.assertEqual(caught.exception.code, 'model_not_configured')

    def test_lost_create_response_preserves_key_without_retry(self):
        self.reply = lambda *_: (None, None)
        with self.assertRaises(sdk.SDKError) as caught:
            self.client.authoring.create(BRIEF, idempotency_key='authoring-replay')
        self.assertEqual(caught.exception.idempotency_key, 'authoring-replay')
        self.assertEqual(len(self.requests), 1)

    def test_invalid_authoring_inputs_and_malformed_role_response(self):
        for action in (lambda: self.client.authoring.create([]),
                       lambda: self.client.authoring.status('../elsewhere'),
                       lambda: self.client.authoring.assign(JOB, 'owner', 'provider', 'model', 'session'),
                       lambda: self.client.authoring.assign(JOB, 'writer', '', 'model', 'session'),
                       lambda: self.client.authoring.validate(JOB, timeout=0),
                       lambda: self.client.authoring.freeze(JOB, 'unversioned')):
            with self.assertRaises(ValueError):
                action()
        self.assertEqual(self.requests, [])
        self.reply = lambda *_: (200, {'id': 'd' * 32, 'role': 'writer', 'token': 'invalid'})
        with self.assertRaises(sdk.SDKError) as caught:
            self.client.authoring.assign(JOB, 'writer', 'provider', 'model', 'session')
        self.assertEqual(caught.exception.code, 'invalid_response')
        self.assertEqual(caught.exception.job_id, JOB)


if __name__ == '__main__':
    unittest.main()
