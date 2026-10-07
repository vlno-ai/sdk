"""Fresh authority is required before session mutation or replay."""

import copy
from types import SimpleNamespace
import unittest
from unittest.mock import Mock

from closed_world_sdk import SDKError
from vlno.sessions.identity import endpoint_origin, identity, require_identity


class SessionIdentityTests(unittest.TestCase):
    def setUp(self):
        self.response = {"schema": "vlno.assessment-run-access/1", "orgId": "workspace",
                         "actor": {"kind": "service_key", "reference": "sa_" + "a" * 32}}
        self.transport = SimpleNamespace(endpoint="https://api.example.test/",
                                         api_key="not stored", request=Mock(return_value=self.response))
        self.client = SimpleNamespace(_transport=self.transport)

    def test_current_identity_contains_no_credential_and_is_independent(self):
        saved = identity(self.client)
        self.transport.request.assert_called_once_with("GET", "/v1/assessment-runs/access")
        self.assertEqual(saved["endpoint"], "https://api.example.test")
        self.assertNotIn("not stored", repr(saved))
        self.response["actor"]["reference"] = "sa_" + "b" * 32
        self.assertEqual(saved["actor"]["reference"], "sa_" + "a" * 32)

    def test_actor_workspace_or_endpoint_change_blocks_recovery(self):
        original = identity(self.client)
        for field, value in (("orgId", "other"), ("actor", {"kind": "service_key",
                                                           "reference": "sa_" + "b" * 32})):
            with self.subTest(field=field):
                changed = copy.deepcopy(self.response)
                changed[field] = value
                self.transport.request.return_value = changed
                with self.assertRaisesRegex(SDKError, "session_identity_changed"):
                    require_identity(self.client, original)
        self.transport.request.return_value = self.response
        self.transport.endpoint = "https://other.example.test"
        with self.assertRaisesRegex(SDKError, "session_identity_changed"):
            require_identity(self.client, original)

    def test_revoked_authority_is_not_replaced_by_saved_identity(self):
        original = identity(self.client)
        self.transport.request.side_effect = SDKError("forbidden", status=403)
        with self.assertRaises(SDKError) as caught:
            require_identity(self.client, original)
        self.assertEqual(caught.exception.status, 403)

    def test_malformed_or_extended_identity_is_rejected(self):
        for change in ({"extra": True}, {"schema": "new-version"}, {"orgId": ""},
                       {"actor": {"kind": "service_key", "reference": "secret"}},
                       {"actor": {"kind": "service_key", "reference": "sa_" + "a" * 32,
                                  "claim": "must not enter session"}}):
            with self.subTest(change=change):
                self.transport.request.return_value = {**self.response, **change}
                with self.assertRaisesRegex(SDKError, "session_identity_invalid"):
                    identity(self.client)

    def test_origin_canonicalization_and_private_http_boundary(self):
        for given, expected in (("https://API.example.test:443/", "https://api.example.test"),
                                ("http://127.0.0.1:3100/", "http://127.0.0.1:3100"),
                                ("http://[::1]:80/", "http://[::1]")):
            self.assertEqual(endpoint_origin(given), expected)
        for bad in ("http://api.example.test", "https://user:secret@api.example.test",
                    "https://api.example.test/path", "https://api.example.test?key=secret",
                    "https://api.example.test#part", "https://[fe80::1%en0]", "ftp://example.test"):
            with self.subTest(endpoint=bad), self.assertRaisesRegex(SDKError, "session_endpoint_invalid"):
                endpoint_origin(bad)


if __name__ == "__main__":
    unittest.main()
