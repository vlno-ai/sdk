"""Private local files shared by Python and Go session controllers.

Local macOS/Linux filesystems only. These permissions do not isolate a harness
running as the same operating-system user from its controller.
"""

import os
from pathlib import Path
import re
import secrets
import stat
import sys

from .filesystems import supported_filesystem


class SessionFileError(RuntimeError):
    """A stable diagnostic; never include session contents in the message."""


def private_file(descriptor):
    info = os.fstat(descriptor)
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid()
            or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1):
        raise SessionFileError("session_file_not_private")
    return info


class PrivateDirectory:
    """Hold a stable flock inode until close; never delete or break the lock."""

    def __init__(self, path, *, create=False):
        if sys.platform not in {"darwin", "linux"}:
            raise SessionFileError("session_platform_unsupported")
        import fcntl
        self.path = Path(path).absolute()
        self.fd = self.lock = None
        try:
            if create:
                self.path.mkdir(mode=0o700)
                parent = os.open(self.path.parent, os.O_RDONLY | os.O_DIRECTORY)
                try:
                    os.fsync(parent)
                finally:
                    os.close(parent)
            self.fd = os.open(self.path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            if not supported_filesystem(self.fd):
                raise SessionFileError("session_filesystem_unsupported")
            info = os.fstat(self.fd)
            if info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o700:
                raise SessionFileError("session_directory_not_private")
            self.lock = self._open("lock", os.O_RDWR | os.O_CREAT)
            private_file(self.lock)
            try:
                fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise SessionFileError("session_in_use") from None
            self.check()
        except BaseException:
            self.close()
            raise

    def _open(self, name, flags):
        if not isinstance(name, str) or not re.fullmatch(r"[a-z.][a-z0-9.-]{0,63}", name) or name in {".", ".."}:
            raise SessionFileError("session_filename_invalid")
        return os.open(name, flags | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK,
                       0o600, dir_fd=self.fd)

    def check(self):
        """Refuse a replaced directory or lock rather than creating a second owner."""
        if self.fd is None or self.lock is None:
            raise SessionFileError("session_closed")
        directory = os.stat(self.path, follow_symlinks=False)
        lock = os.stat("lock", dir_fd=self.fd, follow_symlinks=False)
        for actual, expected in ((directory, os.fstat(self.fd)),
                                 (lock, private_file(self.lock))):
            if (actual.st_dev, actual.st_ino) != (expected.st_dev, expected.st_ino):
                raise SessionFileError("session_path_changed")
        if stat.S_IMODE(directory.st_mode) != 0o700:
            raise SessionFileError("session_directory_not_private")

    def read(self, name, maximum):
        self.check()
        descriptor = self._open(name, os.O_RDONLY)
        try:
            info = private_file(descriptor)
            if info.st_size > maximum:
                raise SessionFileError("session_file_too_large")
            with os.fdopen(descriptor, "rb", closefd=False) as stream:
                value = stream.read(maximum + 1)
            if len(value) > maximum:
                raise SessionFileError("session_file_too_large")
            return value
        finally:
            os.close(descriptor)

    def write_new(self, name, value):
        """Persist an immutable file before any operation can depend on it."""
        self.check()
        descriptor = self._open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL)
        try:
            private_file(descriptor)
            self._write(descriptor, value)
        finally:
            os.close(descriptor)
        os.fsync(self.fd)

    def replace(self, name, value):
        """Durable replacement. Any fsync error must stop subsequent API dispatch."""
        self.check()
        old = self._open(name, os.O_RDONLY)
        try:
            private_file(old)
        finally:
            os.close(old)
        temporary = ".pending-" + secrets.token_hex(16)
        descriptor = self._open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL)
        try:
            private_file(descriptor)
            self._write(descriptor, value)
            self.check()
            os.replace(temporary, name, src_dir_fd=self.fd, dst_dir_fd=self.fd)
            os.fsync(self.fd)
        finally:
            os.close(descriptor)
            try:
                os.unlink(temporary, dir_fd=self.fd)
            except FileNotFoundError:
                pass

    @staticmethod
    def _write(descriptor, value):
        remaining = memoryview(value)
        while remaining:
            count = os.write(descriptor, remaining)
            if count == 0:
                raise SessionFileError("session_write_failed")
            remaining = remaining[count:]
        os.fsync(descriptor)

    def close(self):
        for name in ("lock", "fd"):
            descriptor = getattr(self, name, None)
            if descriptor is not None:
                os.close(descriptor)
                setattr(self, name, None)

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()
