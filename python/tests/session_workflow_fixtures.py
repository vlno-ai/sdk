"""Shared bounded local HTTP/session ownership for workflow acceptance."""
from pathlib import Path
import sys
import tempfile
import unittest

from session_http_fixture import SessionHTTP


class WorkflowCase(unittest.TestCase):
    def setUp(self):
        self.http = SessionHTTP()
        self.addCleanup(self.http.close)
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root, self.path = Path(temporary.name), Path(temporary.name) / 'session'
        self.marker = self.root / 'executed'
        self.argv = [sys.executable, '-c',
            f'from pathlib import Path; p=Path({str(self.marker)!r}); p.write_text(p.read_text()+"x" if p.exists() else "x"); print("hello")']

    def run_command(self):
        return self.http.builder(self.path).run_command(self.argv, cwd=self.root, timeout=5)
