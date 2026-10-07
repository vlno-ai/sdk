"""Strict, bounded JSON shared with the Go client session journal."""

import json
import math

from .files import SessionFileError

MAXIMUM = 2 * 1024 * 1024
SAFE_INTEGER = 9007199254740991


def require(condition):
    if not condition:
        raise SessionFileError("session_corrupt")


def record(value, fields):
    require(type(value) is dict and set(value) == set(fields.split()))
    return value


def integer(value, minimum=0, maximum=SAFE_INTEGER):
    require(type(value) is int and minimum <= value <= maximum)


def _tree(value, depth=0):
    require(depth <= 16)
    if type(value) is dict:
        for key, child in value.items():
            require(type(key) is str)
            key.encode("utf-8")
            _tree(child, depth + 1)
    elif type(value) is list:
        for child in value:
            _tree(child, depth + 1)
    elif type(value) is str:
        value.encode("utf-8")
    elif type(value) is int:
        require(abs(value) <= SAFE_INTEGER)
    elif type(value) is float:
        require(math.isfinite(value))
    else:
        require(value is None or type(value) is bool)


def _pairs(pairs):
    value = {}
    for key, child in pairs:
        require(key not in value)
        value[key] = child
    return value


def encode(value):
    try:
        _tree(value)
        encoded = json.dumps(value, ensure_ascii=False, allow_nan=False, separators=(",", ":"))
        # encoding/json escapes these even with SetEscapeHTML(false).
        return encoded.replace("\u2028", "\\u2028").replace("\u2029", "\\u2029").encode("utf-8")
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise SessionFileError("session_corrupt") from None


def decode(raw):
    require(type(raw) is bytes and len(raw) <= MAXIMUM)
    try:
        value = json.loads(raw.decode("utf-8"), object_pairs_hook=_pairs,
                           parse_constant=lambda _: require(False))
        _tree(value)
        require(type(value) is dict)
        return value
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise SessionFileError("session_corrupt") from None
