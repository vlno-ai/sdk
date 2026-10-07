"""Faults preserve exact intent and require validated reopen before dispatch."""

import copy
import os
from unittest.mock import patch

from vlno.sessions.files import SessionFileError
from vlno.sessions.store import SessionStore
from session_store_fixtures import StoreCase, fixture


class SessionStoreTests(StoreCase):
    def test_close_reopen_preserves_claim_and_exact_admission(self):
        self.confirm()
        before = self.store.document
        self.store.close()
        with SessionStore.open(self.path) as reopened:
            self.assertEqual(reopened.document, before)
            self.assertEqual(reopened.claim, fixture("claim")["claim"])
            leaked = reopened.document
            leaked["source"]["assessmentId"] = "changed"
            self.assertEqual(reopened.document, before)

    def test_existing_session_cannot_be_created_again(self):
        self.store.close()
        with self.assertRaises(FileExistsError):
            SessionStore.create(self.path, fixture("prepared"), fixture("claim"))
        with SessionStore.open(self.path) as reopened:
            self.assertEqual(reopened.document["revision"], 1)

    def test_corrupt_or_missing_files_never_generate_replacement_intent(self):
        self.store.close()
        before = (self.path / "claim.json").read_bytes()
        (self.path / "journal.json").write_bytes(b'{"schema":')
        with self.assertRaisesRegex(SessionFileError, "session_corrupt"):
            SessionStore.open(self.path)
        self.assertEqual((self.path / "claim.json").read_bytes(), before)
        (self.path / "journal.json").unlink()
        with self.assertRaisesRegex(SessionFileError, "session_incomplete"):
            SessionStore.open(self.path)
        self.assertFalse((self.path / "journal.json").exists())

    def test_file_sync_failure_poison_requires_reopen_of_old_state(self):
        old = self.store.document
        with patch("vlno.sessions.files.os.fsync", side_effect=OSError("injected")):
            with self.assertRaises(OSError):
                self.store.update(lambda doc: doc["admission"].update(state="unknown"))
        with self.assertRaisesRegex(SessionFileError, "session_closed"):
            self.store.document
        self.store.close()
        with SessionStore.open(self.path) as reopened:
            self.assertEqual(reopened.document, old)

    def test_directory_sync_uncertainty_reopens_same_new_intent(self):
        old = self.store.document
        sync = os.fsync

        def failure(descriptor):
            if descriptor == self.store.files.fd:
                raise OSError("after rename")
            sync(descriptor)

        with patch("vlno.sessions.files.os.fsync", side_effect=failure):
            with self.assertRaises(OSError):
                self.store.update(lambda doc: doc["admission"].update(state="unknown"))
        with self.assertRaisesRegex(SessionFileError, "session_closed"):
            self.store.update(lambda doc: None)
        self.store.close()
        with SessionStore.open(self.path) as reopened:
            current = reopened.document
            self.assertEqual(current["admission"]["state"], "unknown")
            self.assertEqual(current["admission"]["idempotencyKey"], old["admission"]["idempotencyKey"])
            self.assertEqual(current["revision"], 2)

    def test_source_actor_and_replay_identity_cannot_change(self):
        old = self.store.document
        for change in (lambda d: d.update(orgId="other"),
                       lambda d: d["admission"]["actor"].update(reference="sa_" + "b" * 32),
                       lambda d: d["admission"].update(idempotencyKey="99999999-9999-4999-8999-999999999999")):
            with self.assertRaisesRegex(SessionFileError, "session_corrupt"):
                self.store.update(change)
            self.assertEqual(self.store.document, old)

    def test_prepared_cannot_skip_unknown_before_admission_ack(self):
        next_document = fixture("confirmed")
        with self.assertRaisesRegex(SessionFileError, "session_corrupt"):
            self.store.save(next_document)
        self.assertEqual(self.store.document["revision"], 1)

    def test_fifo_and_unsupported_filesystem_are_rejected(self):
        self.store.close()
        (self.path / "journal.json").unlink()
        os.mkfifo(self.path / "journal.json", 0o600)
        with self.assertRaisesRegex(SessionFileError, "session_file_not_private"):
            SessionStore.open(self.path)
        with patch("vlno.sessions.files.supported_filesystem", return_value=False):
            with self.assertRaisesRegex(SessionFileError, "session_filesystem_unsupported"):
                SessionStore.open(self.path)
