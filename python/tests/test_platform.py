"""Customer API and capability boundaries, with no models or Docker."""
import copy
import json
import time
import unittest
from unittest.mock import Mock, patch

from closed_world_sdk import SDKError
from closed_world_sdk.platform import PlatformClient, PlatformCase

RUN = 'cw_' + 'a' * 32
WORLD = 'b' * 32
KEY = 'vlno_live_' + 'c' * 32
TOKEN = 'd' * 40
CLAIM = 'e' * 32


def view(state='running', outcome='not_evaluated', case_state='running'):
    return {'id': RUN, 'engine': 'closed_world', 'state': state,
            'result': {'schema': 'vlno.world-run/1', 'engine': 'closed_world', 'executionState': state,
                       'evaluationStatus': outcome, 'cases': [{'index': 0, 'scenario': 'test-case@1', 'state': case_state}],
                       'summary': {'cleanup': {'status': 'confirmed' if state == 'completed' else 'pending'}}}}


def assigned(state='ready'):
    path = '/v1/world-agent/' + WORLD
    headers = {'Authorization': 'Bearer ' + TOKEN}
    expires = time.time() + 600
    return {'run': view(), 'case': {'worldId': WORLD, 'index': 0, 'scenario': 'test-case@1',
            'task': 'Archive a test note', 'expiresAt': expires, 'state': state,
            'connection': {'schema_version': 1, 'generation': 1, 'world_id': WORLD,
                'task': 'Archive a test note', 'expires_at': expires,
                'mcp': {'transport': 'streamable-http', 'path': path + '/mcp', 'headers': dict(headers)},
                'app': {'path': path + '/call', 'headers': dict(headers)}}}}


class PlatformTests(unittest.TestCase):
    def setUp(self):
        self.client = PlatformClient('https://api.example.test', KEY)
        self.client._transport.request = Mock(return_value=view())
        self.run = self.client.runs.create('test-suite@1', 'rev-1', idempotency_key='stable-key-123')
        self.client._transport.request.reset_mock()

    def test_transport_handles_both_worker_and_product_error_shapes(self):
        client = PlatformClient('https://api.example.test', KEY)
        for body, expected in [
            ({'error': 'Not Found', 'statusCode': 404}, 'http_error'),
            ({'error': {'code': 'world_not_found'}}, 'world_not_found'),
            ({'error': {'code': KEY}}, 'http_error'),
            ({'error': None}, 'http_error'),
            ([], 'http_error'),
        ]:
            class ErrorReply:
                status = 404
                def read(self, limit):
                    return json.dumps(body).encode()
                def __enter__(self):
                    return self
                def __exit__(self, *args):
                    pass
            client._transport.opener.open = Mock(return_value=ErrorReply())
            with self.subTest(body=body), self.assertRaises(SDKError) as caught:
                client.runs.get(RUN)
            self.assertEqual(caught.exception.code, expected)
            self.assertEqual(caught.exception.status, 404)
            self.assertNotIn(KEY, str(caught.exception))

    def test_requires_customer_key_and_secure_origin(self):
        for endpoint, key in [('https://api.test', TOKEN), ('http://api.test', KEY), ('https://api.test/path', KEY), ('https://user:pass@api.test', KEY)]:
            with self.subTest(endpoint=endpoint), self.assertRaises(ValueError):
                PlatformClient(endpoint, key)

    def test_creation_keeps_replay_key(self):
        self.client.runs.create('test-suite@1', 'rev-1', idempotency_key='stable-key-123')
        args, kwargs = self.client._transport.request.call_args
        self.assertEqual(args[:2], ('POST', '/v1/world-runs'))
        self.assertEqual(kwargs['key'], 'stable-key-123')

    def test_retrieval_rejects_worker_ids_and_mismatched_product_ids(self):
        with self.assertRaises(ValueError):
            self.client.runs.get(WORLD)
        other = view(); other['id'] = 'cw_' + 'f' * 32
        self.client._transport.request.return_value = other
        with self.assertRaises(SDKError):
            self.client.runs.get(RUN)

    def test_same_claim_waits_for_ready_and_agent_has_only_scoped_transport(self):
        self.client._transport.request.side_effect = [view(), assigned('creating'), assigned()]
        with patch('closed_world_sdk.platform.time.sleep'):
            case = self.run.next(CLAIM)
        posts = [c for c in self.client._transport.request.call_args_list if c.args[0] == 'POST']
        self.assertEqual([c.args[2] for c in posts], [{'claim': CLAIM}, {'claim': CLAIM}])
        self.assertEqual(case.agent._transport.api_key, TOKEN)
        self.assertNotIn(KEY, repr(case.agent.connection()))
        self.assertFalse(hasattr(case.agent, '_client'))
        self.assertFalse(hasattr(case.agent, 'evaluate'))
        self.assertEqual(case.agent.mcp['url'], f'https://api.example.test/v1/world-agent/{WORLD}/mcp')
        case.agent._transport.request = Mock(return_value={'status': 200})
        case.agent.call('archive_note', note_id=1)
        self.assertEqual(case.agent._transport.request.call_args.args[1], f'/v1/world-agent/{WORLD}/call')
        with self.assertRaises(ValueError):
            case.agent.exec(['true'])

    def test_manifest_is_returned_as_copy(self):
        case = PlatformCase(self.client, RUN, assigned()['case'])
        first = case.agent.connection()
        first['mcp']['headers']['Authorization'] = 'changed'
        self.assertEqual(case.agent.mcp['headers']['Authorization'], 'Bearer ' + TOKEN)

    def test_rejects_credential_redirects_and_malformed_manifests(self):
        mutations = [
            lambda v: v['connection']['mcp'].update(path='https://other.test/mcp'),
            lambda v: v['connection']['app'].update(path='/v1/world-agent/' + 'f' * 32 + '/call'),
            lambda v: v['connection']['mcp'].update(url='https://other.test/mcp'),
            lambda v: v['connection']['app'].update(headers={'Authorization': 'Bearer ' + 'f' * 40}),
            lambda v: v['connection'].update(generation=True),
            lambda v: v.update(expiresAt=0),
            lambda v: v.update(scenario=None),
            lambda v: v['connection'].update(task='wrong task'),
        ]
        for mutate in mutations:
            value = assigned()['case']; mutate(value)
            with self.subTest(mutation=mutate), self.assertRaises(SDKError):
                PlatformCase(self.client, RUN, value)

    def test_account_key_cannot_become_agent_capability(self):
        value = assigned()['case']
        for name in ('mcp', 'app'):
            value['connection'][name]['headers']['Authorization'] = 'Bearer ' + KEY
        with self.assertRaises(SDKError):
            PlatformCase(self.client, RUN, value)

    def test_finish_polls_and_preserves_inconclusive_grade(self):
        result = view('completed', 'inconclusive', 'inconclusive')
        self.client._transport.request.side_effect = [view(case_state='finishing'), result]
        with patch('closed_world_sdk.platform.time.sleep'):
            value = self.run.finish(0, CLAIM)
        self.assertEqual(value['result']['evaluationStatus'], 'inconclusive')
        self.assertEqual([c.args[0] for c in self.client._transport.request.call_args_list], ['POST', 'GET'])

    def test_does_not_retry_uncertain_finish_or_hide_run_identity(self):
        self.client._transport.request.side_effect = SDKError('transport_error_outcome_unknown')
        with self.assertRaises(SDKError) as caught:
            self.run.finish(0, CLAIM)
        self.assertEqual(caught.exception.run_id, RUN)
        self.client._transport.request.assert_called_once()

    def test_terminal_run_does_not_claim_or_rerun_agent(self):
        self.client._transport.request.return_value = view('completed', 'passed', 'passed')
        self.assertIsNone(self.run.next(CLAIM))
        self.assertEqual(self.client._transport.request.call_args.args[0], 'GET')

    def test_cancellation_is_a_request_not_cleanup_confirmation(self):
        self.client._transport.request.return_value = view('cancelling')
        self.assertEqual(self.run.cancel()['state'], 'cancelling')
        self.client._transport.request.return_value = view('cancelled', 'inconclusive', 'not_run')
        self.assertEqual(self.run.wait()['evaluationStatus'], 'inconclusive')

    def test_validates_before_sending(self):
        for value in (0, -1, True, float('inf'), float('nan')):
            with self.subTest(timeout=value), self.assertRaises(ValueError):
                self.run.next(CLAIM, timeout=value)
        with self.assertRaises(ValueError):
            self.run.finish(True, CLAIM)
        with self.assertRaises(ValueError):
            self.run.next('short')
        self.client._transport.request.assert_not_called()

class EvidenceTests(unittest.TestCase):
    def setUp(self):
        import hashlib
        self.data = b'{"test":"sealed observation"}'
        self.client = PlatformClient('https://api.example.test', KEY)
        from closed_world_sdk.platform import PlatformRun
        self.run = PlatformRun(self.client, view('completed', 'passed', 'passed'))
        self.manifest = {'schema':'vlno.world-evidence/1', 'runId':RUN, 'status':'complete', 'evidenceComplete':False,
                         'artifacts':[{'caseIndex':0, 'kind':'trace', 'sha256':hashlib.sha256(self.data).hexdigest(),
                                       'evaluationId':WORLD, 'size':len(self.data), 'state':'archived', 'archivedAt':'2026-09-27T00:00:00Z'}]}
        self.client._transport.request = Mock(return_value=self.manifest)

    def test_verifies_exact_bytes_without_reserializing(self):
        data = self.data
        class Reply:
            status = 200
            def read(self, limit): return data[:limit]
            def __enter__(self): return self
            def __exit__(self, *args): pass
        self.client._transport.opener.open = Mock(return_value=Reply())
        self.assertEqual(self.run.artifact(0,'trace'), data)
        self.assertFalse(self.run.evidence()['evidenceComplete'])
        self.client._transport.opener.open.assert_called_once()

    def test_corrupt_or_oversized_download_never_returns_bytes(self):
        for data in (b'x' * len(self.data), self.data+b'extra'):
            class Reply:
                status = 200
                def read(self, limit): return data[:limit]
                def __enter__(self): return self
                def __exit__(self, *args): pass
            self.client._transport.opener.open = Mock(return_value=Reply())
            with self.assertRaises(SDKError) as caught:
                self.run.artifact(0,'trace')
            self.assertEqual(caught.exception.code, 'artifact_integrity_error')
            self.assertEqual(caught.exception.run_id, RUN)

    def test_wrong_identity_duplicates_pending_and_missing_receipts_fail_closed(self):
        for mutate in (
            lambda v: v.update(runId='cw_'+'f'*32),
            lambda v: v['artifacts'].append(copy.deepcopy(v['artifacts'][0])),
            lambda v: v['artifacts'][0].update(size=17*1024*1024),
            lambda v: v['artifacts'][0].update(state='pending'),
        ):
            value=copy.deepcopy(self.manifest);mutate(value)
            self.client._transport.request.return_value=value
            with self.assertRaises(SDKError) as caught: self.run.artifact(0,'trace')
            self.assertEqual(caught.exception.code,'invalid_evidence_manifest')
        self.client._transport.request.return_value={**self.manifest,'status':'pending','artifacts':[{**self.manifest['artifacts'][0],'state':'pending','size':None}]}
        with self.assertRaises(SDKError) as caught: self.run.artifact(0,'trace')
        self.assertEqual(caught.exception.code,'artifact_not_archived')
        with self.assertRaises(SDKError) as caught: self.run.artifact(0,'snapshot')
        self.assertEqual(caught.exception.code,'artifact_unavailable')


if __name__ == '__main__':
    unittest.main()
