"""Locked durable state. This layer never dispatches requests or starts a process."""

import copy
import threading

from .fields import now
from .files import PrivateDirectory, SessionFileError
from .json import encode, require, MAXIMUM
from .schema import parse, validate_claim
from .transitions import exact, initial, transition


class SessionStore:
    def __init__(self, files):
        self.files = files
        self._lock = threading.RLock()
        self._failed = False

    @classmethod
    def create(cls, path, document, claim):
        initial(document)
        claim_bytes = encode(claim)
        validate_claim(claim_bytes, document)
        files = PrivateDirectory(path, create=True)
        try:
            files.write_new("claim.json", claim_bytes)
            files.write_new("journal.json", encode(document))
            return cls(files)
        except BaseException:
            files.close()
            raise

    @classmethod
    def open(cls, path):
        instance = cls(PrivateDirectory(path))
        try:
            instance.document
            return instance
        except BaseException:
            instance.close()
            raise

    def _read(self):
        if self._failed:
            raise SessionFileError("session_closed")
        try:
            value = parse(self.files.read("journal.json", MAXIMUM))
        except FileNotFoundError:
            raise SessionFileError("session_incomplete") from None
        try:
            claim = self.files.read("claim.json", 1024)
        except FileNotFoundError:
            raise SessionFileError("session_corrupt") from None
        return value, validate_claim(claim, value)

    @property
    def document(self):
        with self._lock:
            return self._read()[0]

    @property
    def claim(self):
        with self._lock:
            return self._read()[1]

    def _save(self, old, candidate):
        transition(old, candidate)
        require(exact(self._read()[0], old))
        try:
            self.files.replace("journal.json", encode(candidate))
        except BaseException:
            # Rename/fsync may have succeeded despite an error. No caller may
            # dispatch using this handle; a new open must inspect actual bytes.
            self._failed = True
            raise

    def save(self, candidate):
        with self._lock:
            self._save(self._read()[0], copy.deepcopy(candidate))

    def update(self, change):
        with self._lock:
            old = self._read()[0]
            candidate = copy.deepcopy(old)
            change(candidate)
            candidate["revision"] = old["revision"] + 1
            candidate["updatedAt"] = now()
            self._save(old, candidate)
            return copy.deepcopy(candidate)

    def close(self):
        with self._lock:
            self.files.close()

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()
