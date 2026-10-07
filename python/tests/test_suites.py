"""Suite callback and recovery behavior over the shared local HTTP fixture."""
import time
import unittest
from types import SimpleNamespace
from unittest.mock import patch

from suite_fixtures import SuiteProtocol, base, sdk, RUN, WORLD, TOKEN, CLAIM, metadata


class Suites1Test(SuiteProtocol, unittest.TestCase):
    def test_callback_receives_only_public_inputs_and_scoped_capability(self):
        self.protocol(world_state='creating')
        inputs = []
        def agent(case):
            inputs.append(case)
            self.assertEqual(case._fields, ('task', 'app', 'case_ref'))
            self.assertEqual(case.task, 'Archive the release note.')
            self.assertEqual(case.case_ref, 'archive-note@1')
            self.assertFalse(hasattr(case, 'evaluate'))
            self.assertFalse(hasattr(case.app, 'setup'))
            self.assertNotIn(TOKEN, repr(case))
            self.assertNotIn(base.KEY, repr(case))
            with self.assertRaises(AttributeError):
                case.task = 'changed'
            case.app.operations()
            case.app.call('archive_note', note_id=3, archived=True)
            return {'status': 'passed'}  # Ignored; worker decides.
        result = self.client.suites.run('notes@1', agent, 'commit-123', idempotency_key='suite-run-123')
        self.assertEqual(result['state'], 'completed')
        self.assertEqual(len(inputs), 1)
        self.assertEqual(self.world_polls, 1)
        self.assertNotIn(TOKEN, repr(result))
        for method, path, payload, headers in self.requests:
            expected = TOKEN if path.endswith(('/task', '/call', '/operations')) else base.KEY
            self.assertEqual(headers['Authorization'], 'Bearer ' + expected)
        create = self.requests[0]
        self.assertEqual(create[2], {'suite': 'notes@1', 'revision': 'commit-123', 'ttl_seconds': 1800})
        self.assertEqual(create[3]['Idempotency-Key'], 'suite-run-123')
        next_request = next(r for r in self.requests if r[1].endswith('/next'))
        finish = next(r for r in self.requests if r[1].endswith('/finish'))
        self.assertEqual(finish[2], {'case_index': 0, 'claim': next_request[2]['claim'], 'agent_status': 'completed'})
        self.assertNotIn('claim', result)


    def test_callback_failure_is_recorded_without_exception_text(self):
        self.protocol(result='error')
        def agent(_):
            raise RuntimeError('SECRET_CUSTOMER_EXCEPTION')
        result = self.client.suites.run('notes@1', agent, 'v1')
        self.assertEqual(result['cases'][0]['state'], 'error')
        finish = next(r for r in self.requests if r[1].endswith('/finish'))
        self.assertEqual(finish[2]['agent_status'], 'error')
        self.assertNotIn('SECRET_CUSTOMER_EXCEPTION', repr(self.requests))
        self.assertFalse(any(r[1].endswith('/cancel') for r in self.requests))


    def test_environment_opt_in_reaches_suite_creation(self):
        self.protocol()
        self.client.suites.run('notes@1', lambda _: None, 'v1', environment=True)
        self.assertIs(self.requests[0][2]['environment'], True)
        for invalid in (1, 'true', None):
            with self.assertRaises(ValueError):
                self.client.suites.create('notes@1', 'v1', environment=invalid)


    def test_multiple_cases_are_sequential_and_each_invocation_has_new_claim(self):
        all_claims = []
        for _ in range(2):
            progress = {'index': 0, 'finished': [], 'called': []}
            def state():
                return {'id': RUN, 'state': 'completed' if progress['index'] == 2 else 'running' if progress['index'] else 'pending',
                        'cases': [{'index': n, 'ref': f'case-{n}@1', 'state': 'passed' if n < progress['index'] else 'pending',
                                   'cleanup_confirmed': n < progress['index']} for n in range(2)]}
            def reply(method, path, payload):
                if path == '/v1/suite-runs':
                    return 201, state()
                if path.endswith('/next'):
                    n = progress['index']
                    self.assertEqual(progress['finished'], list(range(n)))
                    all_claims.append(payload['claim'])
                    value = state()
                    value['state'] = 'running'
                    value['cases'][n]['state'] = 'running'
                    return 200, {'run': value, 'case': {'index': n, 'ref': f'case-{n}@1',
                        'world': {'id': WORLD, 'state': 'ready', 'agent_token': TOKEN}}}
                if path.endswith('/task'):
                    return 200, {'task': f"Task {progress['index']}"}
                if path.endswith('/finish'):
                    n = progress['index']
                    self.assertEqual(payload['case_index'], n)
                    self.assertEqual(progress['called'], list(range(n+1)))
                    progress['finished'].append(n)
                    progress['index'] += 1
                    return 200, state()
                raise AssertionError(path)
            self.reply = reply
            report = self.client.suites.run('notes@1', lambda case: progress['called'].append(int(case.task[-1])), 'v1')
            self.assertEqual(report['state'], 'completed')
            self.assertEqual(progress['called'], [0, 1])
        self.assertEqual(all_claims[0], all_claims[1])
        self.assertEqual(all_claims[2], all_claims[3])
        self.assertNotEqual(all_claims[0], all_claims[2])


    def test_failed_world_never_executes_agent(self):
        self.protocol(world_state='failed', result='invalid')
        calls = []
        report = self.client.suites.run('notes@1', calls.append, 'v1')
        self.assertEqual(calls, [])
        self.assertEqual(report['cases'][0]['state'], 'invalid')
        self.assertEqual(next(r[2]['agent_status'] for r in self.requests if r[1].endswith('/finish')), 'error')


    def test_run_replay_returns_completed_and_refuses_active_without_cancellation(self):
        for value in (metadata('completed', 'passed', True), metadata('running', 'running')):
            with self.subTest(state=value['state']):
                self.requests.clear()
                self.reply = lambda *_: (200, value)
                calls = []
                if value['state'] == 'completed':
                    self.assertEqual(self.client.suites.run('notes@1', calls.append, 'v1'), value)
                else:
                    with self.assertRaises(sdk.SDKError) as caught:
                        self.client.suites.run('notes@1', calls.append, 'v1', idempotency_key='replay-key')
                    self.assertEqual(caught.exception.code, 'suite_run_resume_requires_explicit_control')
                    self.assertEqual(caught.exception.run_id, RUN)
                    self.assertEqual(caught.exception.idempotency_key, 'replay-key')
                self.assertEqual(calls, [])
                self.assertEqual(len(self.requests), 1)


    def test_lost_create_and_next_are_never_retried(self):
        for lost_path in ('/v1/suite-runs', f'/v1/suite-runs/{RUN}/next'):
            with self.subTest(path=lost_path):
                self.protocol()
                delegate = self.reply
                self.reply = lambda method, path, payload: (None, None) if path == lost_path else delegate(method, path, payload)
                self.requests.clear()
                with self.assertRaises(sdk.SDKError) as caught:
                    self.client.suites.run('notes@1', lambda _: self.fail('agent executed'), 'v1', idempotency_key='lost-write')
                self.assertEqual(caught.exception.code, 'transport_error_outcome_unknown')
                self.assertEqual(caught.exception.idempotency_key, 'lost-write')
                self.assertEqual(sum(r[1] == lost_path for r in self.requests), 1)
                self.assertEqual(caught.exception.run_id, None if lost_path == '/v1/suite-runs' else RUN)


    def test_claim_race_does_not_cancel_another_runner(self):
        self.protocol()
        delegate = self.reply
        self.reply = lambda m, p, v: (409, {'error': {'code': 'case_already_claimed'}}) if p.endswith('/next') else delegate(m, p, v)
        with self.assertRaises(sdk.SDKError) as caught:
            self.client.suites.run('notes@1', lambda _: self.fail('agent executed'), 'v1')
        self.assertEqual(caught.exception.code, 'case_already_claimed')
        self.assertFalse(any(r[1].endswith('/cancel') for r in self.requests))


    def test_timeout_after_callback_cancels_without_claiming_interrupt(self):
        self.protocol()
        calls = []
        tick = [100.0]
        clock = SimpleNamespace(monotonic=lambda: tick[0], sleep=time.sleep)
        def agent(case):
            calls.append(case)
            tick[0] = 131.0
        with patch.object(sdk, 'time', clock), self.assertRaises(sdk.SDKError) as caught:
            self.client.suites.run('notes@1', agent, 'v1', timeout=30)
        self.assertEqual(caught.exception.code, 'suite_run_timeout')
        self.assertEqual(len(calls), 1)
        self.assertTrue(any(r[1].endswith('/cancel') for r in self.requests))
        self.assertFalse(any(r[1].endswith('/finish') for r in self.requests))
