import copy
import unittest
from unittest.mock import Mock
from vlno import Client, SDKError


class HostedSuitesTests(unittest.TestCase):
    def setUp(self):
        self.client = Client('https://api.example.test', 'vlno_live_' + 'a' * 48)
        self.suite = {'suite': 'notes@1', 'suiteSha256': 'a' * 64,
                      'title': 'Notes', 'description': 'Archive the requested note.',
                      'limitations': ['No broad security claim.'], 'environmentAllowed': False,
                      'harnesses': [], 'transports': ['app', 'mcp']}
        self.client._transport.request = Mock(return_value={'items': [self.suite]})

    def test_lists_granted_versions_without_requiring_worker_access(self):
        self.assertEqual(self.client.suites.list(), [self.suite])
        self.client._transport.request.assert_called_once_with('GET', '/v1/world-suites')
        self.client._transport.request.return_value = {'items': []}
        self.assertEqual(self.client.suites.list(), [])

    def test_rejects_ambiguous_or_malformed_capabilities(self):
        variants = [None, {}, {'items': None}, {'items': [self.suite, self.suite]}]
        for key, value in [('suite', 'notes'), ('suiteSha256', 'bad'), ('environmentAllowed', 'true'),
                           ('harnesses', ['admin']), ('limitations', []), ('transports', ['ssh'])]:
            item = copy.deepcopy(self.suite); item[key] = value
            variants.append({'items': [item]})
        for value in variants:
            with self.subTest(value=value):
                self.client._transport.request.return_value = value
                with self.assertRaises(SDKError) as caught:
                    self.client.suites.list()
                self.assertEqual(caught.exception.code, 'invalid_suite_catalog')
