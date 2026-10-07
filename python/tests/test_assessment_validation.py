"""Uncertain or malformed API data must not become a successful readiness claim."""

import unittest

from vlno import SDKError
from vlno.assessment_preparation import preparation
from vlno.assessment_status import assessment_status
from test_platform import RUN
from assessment_fixtures import ASSESSMENT, REVISION, readiness, status


class ResponseTests(unittest.TestCase):
    def test_preparation_rejects_missing_duplicate_or_contradictory_groups(self):
        changes = [
            lambda v: v["checks"].pop(),
            lambda v: v["checks"][1].update(group="system"),
            lambda v: v["checks"][0].update(blockers=["source_changed"]),
            lambda v: v["checks"][0].update(state="blocked", blockers=[]),
            lambda v: v.update(state="blocked"),
            lambda v: v["support"].update(modelAttested=True),
            lambda v: v["limits"].update(capacityReserved=True),
            lambda v: v["limits"].update(requestedMaxCostUsd="100.01"),
        ]
        for change in changes:
            value = readiness()
            change(value)
            with self.assertRaises(SDKError):
                preparation(value, ASSESSMENT, REVISION)

    def test_entry_errors_are_not_preparation_blockers(self):
        for blocker in ["scope_not_supported", "approval_required"]:
            value = readiness()
            value["state"] = "blocked"
            value["checks"][0].update(state="blocked", blockers=[blocker])
            with self.assertRaises(SDKError):
                preparation(value, ASSESSMENT, REVISION)

    def test_confirmation_timestamp_does_not_assume_monotonic_wall_clock(self):
        value = status()
        value["lastConfirmedAt"] = "2026-10-07T08:59:59.000Z"
        self.assertEqual(assessment_status(value, RUN), value)

    def test_status_rejects_claim_window_above_ten_minutes(self):
        value = status()
        value["claimBefore"] = "2026-10-07T09:10:01.000Z"
        with self.assertRaises(SDKError):
            assessment_status(value, RUN)

    def test_corrupt_shapes_are_safe_sdk_errors(self):
        for value in [None, [], {"schema": []}, {**status(), "phase": []}]:
            with self.assertRaises(SDKError):
                assessment_status(value, RUN)

    def test_inconclusive_result_cannot_claim_verified_semantics(self):
        value = status()
        value["outcome"] = {
            "status": "inconclusive",
            "assurance": "trusted_worker",
            "scope": "final_target_archived_and_peer_final_state",
            "semanticCheck": "verified_from_sealed_worker_receipts",
            "observationAuthority": "trusted_worker",
        }
        with self.assertRaises(SDKError):
            assessment_status(value, RUN)
        value["outcome"]["semanticCheck"] = "not_available"
        self.assertEqual(assessment_status(value, RUN), value)

    def test_incomplete_evidence_and_unavailable_sync_remain_distinct(self):
        value = status()
        value["synchronization"] = "unavailable"
        value["evidence"] = {
            "state": "incomplete",
            "historyAvailable": True,
            "observationsAvailable": False,
        }
        self.assertEqual(assessment_status(value, RUN), value)
        value["evidence"]["observationsAvailable"] = True
        with self.assertRaises(SDKError):
            assessment_status(value, RUN)
