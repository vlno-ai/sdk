"""Cancelled unallocated runs must remain observable without fabricated results."""
import json
from pathlib import Path
import unittest
from unittest.mock import Mock

from closed_world_sdk import SDKError
from closed_world_sdk.platform import PlatformClient
from test_platform import CLAIM, KEY, RUN, view


class PlatformTerminalTests(unittest.TestCase):
    def setUp(self):
        self.client = PlatformClient('https://api.example.test', KEY)
        self.client._transport.request = Mock()

    def test_shared_null_result_contract(self):
        fixture = Path(__file__).resolve().parents[2] / 'testdata/terminal-without-result.json'
        for case in json.loads(fixture.read_text()):
            with self.subTest(case=case):
                response = {'id': RUN, 'engine': 'closed_world', 'result': None,
                            **{key: value for key, value in case.items() if key != 'valid'}}
                self.client._transport.request.return_value = response
                if case['valid']:
                    self.assertEqual(self.client.runs.get(RUN).info, response)
                else:
                    with self.assertRaises(SDKError):
                        self.client.runs.get(RUN)

    def test_cancelled_before_allocation_never_claims_or_invents_case(self):
        self.client._transport.request.return_value = {
            'id': RUN, 'engine': 'closed_world', 'state': 'cancelled',
            'cancellationRequested': True, 'result': None,
        }
        run = self.client.runs.get(RUN)
        self.assertIsNone(run.next(CLAIM))
        self.assertIsNone(run.wait())
        self.assertTrue(all(call.args[0] == 'GET'
                            for call in self.client._transport.request.call_args_list))

    def test_nonnull_contradictory_result_still_rejected(self):
        response = view('running')
        response.update(state='cancelled', cancellationRequested=True)
        self.client._transport.request.return_value = response
        with self.assertRaises(SDKError):
            self.client.runs.get(RUN)


if __name__ == '__main__':
    unittest.main()
