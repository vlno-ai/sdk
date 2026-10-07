"""Both clients consume the same bounded journal and origin fixtures."""

import copy
import json
import unittest

from vlno.sessions.files import SessionFileError
from vlno.sessions.identity import endpoint_origin
from vlno.sessions.json import decode, encode
from vlno.sessions.schema import parse, validate, validate_claim
from session_store_fixtures import FIXTURES, fixture


class SessionSchemaTests(unittest.TestCase):
    def test_shared_valid_journals_and_private_claim(self):
        for name in ("prepared", "confirmed"):
            document = parse((FIXTURES / (name + ".json")).read_bytes())
            self.assertEqual(validate_claim((FIXTURES / "claim.json").read_bytes(), document), "A" * 43)

    def test_ambiguous_unbounded_or_invalid_json_is_rejected(self):
        for raw in (b'{"x":1,"\\u0078":2}', b'{"x":NaN}', b'{"x":1e999}',
                    b'{"x":9007199254740992}', b'{"x":"\\ud800"}', b'{"x":"\\udc00"}',
                    b'{"x":"\xff"}', b'{}{}', b'{"x":'+b'['*17+b'0'+b']'*17+b'}',
                    b' '*(2*1024*1024+1)):
            with self.subTest(raw=raw[:40]), self.assertRaises(SessionFileError):
                decode(raw)
        self.assertEqual(decode(b'{"x":-1,"y":1e100,"z":"\\ud83c\\udf0f"}'),
                         {"x": -1, "y": 1e100, "z": "🌏"})

    def test_unknown_fields_and_cross_record_contradictions_fail(self):
        mutations = (
            lambda d: d.update(extra="secret"),
            lambda d: d.update(revision=True),
            lambda d: d["admission"].update(state="confirmed"),
            lambda d: d["claim"].update(state="ready"),
            lambda d: d["capture"].update(state="complete"),
            lambda d: d["execution"].update(state="running", pid=123),
            lambda d: d["finish"].update(state="accepted"),
            lambda d: d["cancellation"].update(state="requested"),
        )
        for mutation in mutations:
            document = fixture("prepared")
            mutation(document)
            with self.assertRaisesRegex(SessionFileError, "session_corrupt"):
                validate(document)

    def test_claim_encoding_and_session_identity_are_bound(self):
        for change in ({"sessionId": "99999999-9999-4999-8999-999999999999"},
                       {"claim": "B" * 43}, {"extra": "value"}):
            with self.subTest(change=change), self.assertRaises(SessionFileError):
                validate_claim(encode({**fixture("claim"), **change}), fixture("prepared"))

    def test_transport_envelope_fixture_rejected_before_dispatch(self):
        document = fixture("confirmed")
        document["claim"].update(state="ready", actor=document["admission"]["actor"], case={
            "index": 0, "scenario": "archive-note@1", "worldId": "b" * 32,
            "generation": 1, "expiresAt": "2026-10-07T09:02:00.000Z"})
        event = fixture("invalid-event-envelope")
        document["capture"].update(state="open", nextSequence=2,
                                    events=[{"sequence": 1, "event": event}])
        self.assertLess(len(encode(event)), 24000)
        self.assertGreater(len(json.dumps({"claim": "A" * 43, "events": [event]}).encode()), 65536)
        with self.assertRaisesRegex(SessionFileError, "session_corrupt"):
            validate(document)
        event["data"]["text"] = "🌏" * 3000
        validate(document)

    def test_completed_capture_requires_exact_final_marker(self):
        from session_capture_fixtures import SinkStore
        document = SinkStore().document
        document["execution"].update(state="exited", launchId="99999999-9999-4999-8999-999999999999",
                                     pid=123, exitCode=0, finishedAt=document["updatedAt"])
        event = {"id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "kind": "lifecycle",
                 "occurredAt": document["updatedAt"],
                 "data": {"event": "capture_finished", "exitCode": 0, "streams": ["stdout", "stderr"]}}
        document["capture"].update(state="complete", nextSequence=2, acknowledgedThrough=1,
                                   events=[{"sequence": 1, "event": event}])
        validate(document)
        event["data"]["extra"] = True
        with self.assertRaisesRegex(SessionFileError, "session_corrupt"):
            validate(document)

    def test_shared_origin_cases(self):
        cases = fixture("origins")
        for case in cases["valid"]:
            self.assertEqual(endpoint_origin(case["input"]), case["origin"])
        for value in cases["invalid"]:
            with self.subTest(origin=value), self.assertRaises(Exception):
                endpoint_origin(value)

    def test_shared_encoding_retains_unicode_and_escapes_separators(self):
        self.assertEqual(encode({"text": "🌏\u2028\u2029<"}),
                         '{"text":"🌏\\u2028\\u2029<"}'.encode())
