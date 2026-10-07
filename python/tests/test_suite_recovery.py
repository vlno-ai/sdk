"""Suite callback and recovery behavior over the shared local HTTP fixture."""
import time
import unittest
from types import SimpleNamespace
from unittest.mock import patch

from suite_fixtures import SuiteProtocol, base, sdk, RUN, WORLD, TOKEN, CLAIM, metadata


class Suites2Test(SuiteProtocol, unittest.TestCase):
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
