"""Approved scope consumption; no agent invocation or automatic write recovery."""

import unittest
from unittest.mock import Mock

from vlno import Client, SDKError, ApprovedRun
from test_platform import KEY, RUN
from assessment_fixtures import (
    ASSESSMENT,
    REVISION,
    APPROVAL,
    VERSION,
    COMMAND,
    REPLAY,
    readiness,
    admitted,
    status,
    lifecycle,
)


class AdmissionTests(unittest.TestCase):
    def setUp(self):
        self.client = Client("https://api.example.test", KEY)
        self.request = self.client._transport.request = Mock()

    def admit(self):
        return self.client.assessments.admit(
            ASSESSMENT,
            plan_revision_id=REVISION,
            approval_review_id=APPROVAL,
            system_revision_id=VERSION,
            idempotency_key=REPLAY,
        )

    def test_readiness_is_distinct_from_current_permission_and_is_copied(self):
        raw = readiness(False)
        self.request.return_value = raw
        result = self.client.assessments.prepare(ASSESSMENT, REVISION)
        self.assertEqual(result["state"], "ready")
        self.assertFalse(result["admissionAccess"]["canAdmit"])
        result["checks"][0]["state"] = "blocked"
        self.assertEqual(raw["checks"][0]["state"], "ready")

    def test_admission_returns_exact_receipt_without_followup_get(self):
        self.request.return_value = admitted()
        receipt = self.admit()
        self.assertEqual(receipt["runId"], RUN)
        self.request.assert_called_once_with(
            "POST", f"/v1/assessments/{ASSESSMENT}/runs", COMMAND, key=REPLAY
        )

    def test_unknown_write_is_not_retried_and_manual_replay_preserves_key(self):
        self.request.side_effect = [
            SDKError("transport_error_outcome_unknown", idempotency_key=REPLAY),
            admitted(),
        ]
        with self.assertRaises(SDKError) as caught:
            self.admit()
        self.assertEqual(caught.exception.idempotency_key, REPLAY)
        self.assertEqual(self.request.call_count, 1)
        self.assertEqual(self.admit()["runId"], RUN)
        self.assertEqual(self.request.call_args_list[0], self.request.call_args_list[1])

    def test_rejects_mismatched_source_command_or_key_without_adopting_run(self):
        changes = [
            lambda v: v["source"].update(assessmentId=VERSION),
            lambda v: v["submitted"].update(systemRevisionId=APPROVAL),
            lambda v: v.update(idempotencyKey="other-saved-key"),
            lambda v: v.update(reservedAttempts=True),
            lambda v: v.update(claimBefore="2026-10-07T09:10:01.000Z"),
            lambda v: v.update(executionStarted=True),
        ]
        for change in changes:
            value = admitted()
            change(value)
            self.request.return_value = value
            with self.assertRaises(SDKError) as caught:
                self.admit()
            self.assertEqual(caught.exception.idempotency_key, REPLAY)
            self.assertEqual(caught.exception.code, "admission_outcome_unknown")

    def test_get_run_attaches_only_after_lifecycle_and_approved_detail_validate(self):
        self.request.side_effect = [lifecycle(), status()]
        result = self.client.assessments.get_run(RUN)
        self.assertIsInstance(result, ApprovedRun)
        self.assertEqual(
            [c.args[:2] for c in self.request.call_args_list],
            [
                ("GET", f"/v1/world-runs/{RUN}"),
                ("GET", f"/v1/world-runs/{RUN}/assessment-run"),
            ],
        )
        bad = status()
        bad["id"] = "cw_" + "f" * 32
        self.request.side_effect = [lifecycle(), bad]
        with self.assertRaises(SDKError):
            self.client.assessments.get_run(RUN)

    def test_bad_identifiers_never_reach_transport(self):
        with self.assertRaises(ValueError):
            self.client.assessments.prepare("../other-org", REVISION)
        with self.assertRaises(ValueError):
            self.client.assessments.get_run("a" * 32)
        self.request.assert_not_called()

    def test_oversized_write_reply_preserves_recovery_key_and_uncertainty(self):
        self.request.side_effect = SDKError("response_too_large")
        with self.assertRaises(SDKError) as caught:
            self.admit()
        self.assertEqual(caught.exception.code, "admission_outcome_unknown")
        self.assertEqual(caught.exception.idempotency_key, REPLAY)
        self.request.assert_called_once()

    def test_explicit_authorization_failure_stays_a_failure_without_retry(self):
        original = SDKError("forbidden", status=403)
        self.request.side_effect = original
        with self.assertRaises(SDKError) as caught:
            self.admit()
        self.assertIs(caught.exception, original)
        self.assertEqual(original.idempotency_key, REPLAY)
        self.request.assert_called_once()
