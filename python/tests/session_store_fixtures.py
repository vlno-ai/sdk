"""Shared journal fixtures and local-store lifecycle for recovery tests."""

import copy
import json
from pathlib import Path
import tempfile
import unittest

from vlno.sessions.store import SessionStore

FIXTURES = Path(__file__).resolve().parents[2] / "testdata/session"


def fixture(name):
    return json.loads((FIXTURES / (name + ".json")).read_text())


class StoreCase(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.path = Path(temporary.name) / "session"
        self.store = SessionStore.create(self.path, fixture("prepared"), fixture("claim"))
        self.addCleanup(self.store.close)

    def confirm(self):
        self.store.update(lambda doc: doc["admission"].update(state="unknown"))
        self.store.update(lambda doc: doc["admission"].update(
            state="confirmed", receipt=copy.deepcopy(fixture("confirmed")["admission"]["receipt"])))
