"""Filesystem failures must not silently create replacement execution intent."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from vlno.sessions.files import PrivateDirectory, SessionFileError


@unittest.skipUnless(sys.platform in {"darwin", "linux"}, "local POSIX sessions")
class SessionFilesTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.path = Path(self.temporary.name) / "session"
        self.files = PrivateDirectory(self.path, create=True)
        self.addCleanup(self.files.close)

    def test_exclusive_ownership_survives_another_process_open(self):
        script = """
import sys
from vlno.sessions.files import PrivateDirectory, SessionFileError
try:
    PrivateDirectory(sys.argv[1])
except SessionFileError as error:
    print(error)
    sys.exit(4)
raise AssertionError('second owner acquired lock')
"""
        child = subprocess.run([sys.executable, "-c", script, str(self.path)],
                               capture_output=True, text=True, timeout=10)
        self.assertEqual(child.returncode, 4, child.stderr)
        self.assertEqual(child.stdout.strip(), "session_in_use")
        inode = (self.path / "lock").stat().st_ino
        self.files.close()
        with PrivateDirectory(self.path) as reopened:
            self.assertEqual(os.fstat(reopened.lock).st_ino, inode)

    def test_atomic_replace_and_strict_bounds(self):
        self.files.write_new("journal.json", b'{"revision":0}')
        self.files.replace("journal.json", b'{"revision":1}')
        self.assertEqual(self.files.read("journal.json", 100), b'{"revision":1}')
        with self.assertRaisesRegex(SessionFileError, "too_large"):
            self.files.read("journal.json", 4)
        self.assertEqual((self.path / "journal.json").stat().st_mode & 0o777, 0o600)

    def test_failed_file_sync_keeps_previous_bytes(self):
        self.files.write_new("journal.json", b"old")
        with patch("vlno.sessions.files.os.fsync", side_effect=OSError("disk failure")):
            with self.assertRaises(OSError):
                self.files.replace("journal.json", b"new")
        self.assertEqual(self.files.read("journal.json", 10), b"old")
        self.assertFalse(list(self.path.glob(".pending-*")))

    def test_directory_sync_failure_is_not_acknowledged(self):
        self.files.write_new("journal.json", b"old")
        actual_sync = os.fsync

        def fail_directory(descriptor):
            if descriptor == self.files.fd:
                raise OSError("directory sync failed")
            actual_sync(descriptor)

        with patch("vlno.sessions.files.os.fsync", side_effect=fail_directory):
            with self.assertRaises(OSError):
                self.files.replace("journal.json", b"new")
        # Rename may have succeeded; callers must reload and retain uncertainty.
        self.assertEqual(self.files.read("journal.json", 10), b"new")

    def test_symlink_hardlink_and_permissions_rejected(self):
        self.files.write_new("claim", b"private")
        (self.path / "link").symlink_to(self.path / "claim")
        with self.assertRaises(OSError):
            self.files.read("link", 100)
        os.link(self.path / "claim", self.path / "hard")
        with self.assertRaisesRegex(SessionFileError, "not_private"):
            self.files.read("claim", 100)
        (self.path / "hard").unlink()
        (self.path / "claim").chmod(0o644)
        with self.assertRaisesRegex(SessionFileError, "not_private"):
            self.files.read("claim", 100)

    def test_replaced_lock_and_directory_fail_closed(self):
        (self.path / "lock").rename(self.path / "old-lock")
        with self.assertRaises(OSError):
            self.files.write_new("claim", b"not written")
        self.assertFalse((self.path / "claim").exists())
        (self.path / "old-lock").rename(self.path / "lock")
        self.path.chmod(0o755)
        with self.assertRaisesRegex(SessionFileError, "directory_not_private"):
            self.files.write_new("claim", b"not written")

    def test_existing_directory_and_intent_are_never_overwritten(self):
        self.files.write_new("claim", b"original")
        with self.assertRaises(FileExistsError):
            self.files.write_new("claim", b"replacement")
        with self.assertRaises(FileExistsError):
            PrivateDirectory(self.path, create=True)
        self.assertEqual(self.files.read("claim", 100), b"original")

    def test_names_cannot_escape_the_session(self):
        for name in ("../secret", "/tmp/secret", "..", ".", "sub/secret"):
            with self.subTest(name=name), self.assertRaisesRegex(SessionFileError, "filename_invalid"):
                self.files.read(name, 100)


if __name__ == "__main__":
    unittest.main()
