"""Only explicit preparation retries reuse the inherited ready-case lifecycle."""

import unittest
from unittest.mock import Mock, patch

from vlno import ApprovedRun, Client, SDKError
from test_platform import CLAIM, KEY, RUN
from assessment_fixtures import connection, lifecycle


class ApprovedRunTests(unittest.TestCase):
    def setUp(self):
        self.client = Client("https://api.example.test", KEY)
        self.request = self.client._transport.request = Mock()
        self.run = ApprovedRun(self.client, lifecycle())

    @patch("vlno.approved_run.time.sleep")
    def test_pending_readiness_reuses_claim_and_returns_existing_scoped_case(
        self, sleep
    ):
        self.request.side_effect = [
            lifecycle(),
            SDKError("run_preparing", status=503),
            connection(),
        ]
        case = self.run.next(CLAIM, timeout=3)
        self.assertEqual(case.run_id, RUN)
        posts = [item for item in self.request.call_args_list if item.args[0] == "POST"]
        self.assertEqual(len(posts), 2)
        self.assertEqual(posts[0].args, posts[1].args)
        self.assertEqual(posts[0].args[2], {"claim": CLAIM})
        self.assertLessEqual(posts[1].kwargs["timeout"], posts[0].kwargs["timeout"])
        self.assertNotEqual(case.agent._transport.api_key, KEY)
        with self.assertRaises(ValueError):
            case.agent.exec(["true"])
        sleep.assert_called_once()

    def test_unknown_or_other_503_never_retries_claim(self):
        for error in [
            SDKError("transport_error_outcome_unknown"),
            SDKError("worker_unavailable", status=503),
            SDKError("run_preparing", status=409),
        ]:
            self.request.reset_mock()
            self.request.side_effect = [lifecycle(), error]
            with self.assertRaises(SDKError) as caught:
                self.run.next(CLAIM)
            self.assertIs(caught.exception, error)
            self.assertEqual(self.request.call_count, 2)

    def test_finish_and_other_mutations_are_not_retried(self):
        self.request.side_effect = SDKError("run_preparing", status=503)
        with self.assertRaises(SDKError):
            self.run.finish(0, CLAIM)
        self.assertEqual(self.request.call_count, 1)

    @patch("vlno.approved_run.time.sleep")
    @patch("vlno.approved_run.time.monotonic", side_effect=[0, 0, 1, 2, 3, 4])
    def test_preparation_deadline_never_becomes_an_infinite_retry(self, clock, sleep):
        self.request.side_effect = SDKError("run_preparing", status=503)
        with self.assertRaises(SDKError) as caught:
            self.run._request("POST", "/next", {"claim": CLAIM}, timeout=2)
        self.assertEqual(caught.exception.code, "world_ready_timeout")
        self.assertEqual(self.request.call_count, 1)

    def test_existing_runs_namespace_keeps_released_controller_behavior(self):
        self.request.return_value = lifecycle()
        old = self.client.runs.get(RUN)
        self.assertNotIsInstance(old, ApprovedRun)
