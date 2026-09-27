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


class SuitesTest(unittest.TestCase):
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
        def agent(case):
            calls.append(case)
            time.sleep(.04)
        with self.assertRaises(sdk.SDKError) as caught:
            self.client.suites.run('notes@1', agent, 'v1', timeout=.03)
        self.assertEqual(caught.exception.code, 'suite_run_timeout')
        self.assertEqual(len(calls), 1)
        self.assertTrue(any(r[1].endswith('/cancel') for r in self.requests))
        self.assertFalse(any(r[1].endswith('/finish') for r in self.requests))

    def test_keyboard_interrupt_cancels_and_is_preserved(self):
        self.protocol()
        def agent(_):
            raise KeyboardInterrupt()
        with self.assertRaises(KeyboardInterrupt):
            self.client.suites.run('notes@1', agent, 'v1')
        self.assertTrue(any(r[1].endswith('/cancel') for r in self.requests))

    def test_cleanup_unconfirmed_fails_closed(self):
        self.protocol(finish_pending=False)
        delegate = self.reply
        self.reply = lambda m, p, v: (200, metadata('completed', 'error', False)) if p.endswith('/finish') else delegate(m, p, v)
        with self.assertRaises(sdk.SDKError) as caught:
            self.client.suites.run('notes@1', lambda _: None, 'v1')
        self.assertEqual(caught.exception.code, 'suite_case_cleanup_unconfirmed')

    def test_low_level_wire_contract_and_comparison(self):
        self.protocol()
        self.client.suites.create('notes@1', 'v1', ttl_seconds=60, idempotency_key='create-key')
        assignment = self.client.suites.next(RUN, CLAIM)
        self.assertEqual(assignment['case']['world']['agent_token'], TOKEN)
        self.client.suites.finish(RUN, 0, CLAIM, 'timeout')
        self.client.suites.status(RUN)
        self.client.suites.cancel(RUN)
        self.reply = lambda *_: (200, {'counts': {'regression': 1}})
        self.assertEqual(self.client.suites.compare(RUN, 'e'*32)['counts']['regression'], 1)
        self.assertEqual(self.requests[-1][:3], ('POST', '/v1/suite-run-comparisons', {'baseline': RUN, 'candidate': 'e'*32}))

    def test_invalid_input_sends_no_requests(self):
        actions = [lambda: self.client.suites.create('bad', 'v1'),
                   lambda: self.client.suites.create('notes@1', 'revision with spaces'),
                   lambda: self.client.suites.create('notes@1', 'v1', ttl_seconds=True),
                   lambda: self.client.suites.create('notes@1', 'v1', idempotency_key='x'),
                   lambda: self.client.suites.next(RUN, 'x'),
                   lambda: self.client.suites.finish(RUN, True, CLAIM),
                   lambda: self.client.suites.finish(RUN, 0, CLAIM, 'pass'),
                   lambda: self.client.suites.status('../bad'),
                   lambda: self.client.suites.compare(RUN, 'bad'),
                   lambda: self.client.suites.run('notes@1', None, 'v1'),
                   lambda: self.client.suites.run('notes@1', lambda _: None, 'v1', timeout=float('inf'))]
        for action in actions:
            with self.subTest(action=action), self.assertRaises(ValueError):
                action()
        self.assertEqual(self.requests, [])

    def test_unknown_states_and_changed_identity_fail_closed(self):
        for value in (metadata('mystery'), dict(metadata(), id='e'*32)):
            with self.subTest(value=value):
                self.reply = lambda *_: (200, value)
                with self.assertRaises(sdk.SDKError) as caught:
                    self.client.suites.status(RUN)
                self.assertEqual(caught.exception.code, 'invalid_response')

    def test_hosted_harness_uses_owner_launch_and_no_callback(self):
        self.protocol()
        original=self.reply
        def reply(method,path,payload):
            if path.endswith('/harness'):
                return 200, {'state':'completed','exit_code':0,'error':None}
            return original(method,path,payload)
        self.reply=reply
        result=self.client.suites.run_harness('notes@1','customer','v1')
        self.assertEqual(result['state'],'completed')
        self.assertEqual(self.requests[0][2]['harness'],'customer')
        self.assertIs(self.requests[0][2]['environment'],True)
        launch=next(r for r in self.requests if r[1].endswith('/harness'))
        self.assertEqual(launch[3]['Authorization'],'Bearer '+base.KEY)


if __name__ == '__main__':
    unittest.main()
