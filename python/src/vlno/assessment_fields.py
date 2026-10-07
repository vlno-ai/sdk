"""Validation shared by approved-assessment API responses."""

import re
from datetime import datetime
from functools import wraps

from closed_world_sdk import SDKError

UUID = re.compile(r"[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}")
RUN = re.compile(r"cw_[a-f0-9]{32}")
KEY = re.compile(r"[A-Za-z0-9_-]{8,128}")
SUPPORT = {
    "kind": "application",
    "profile": "tiny-notes-external/1",
    "application": "Tiny Notes",
    "task": "archive-note@1",
    "transport": "api_proxy_only",
    "plannedAttempts": 1,
    "identityAssurance": "declared_harness",
    "outcomeAssurance": "trusted_worker",
    "modelAttested": False,
    "externalProcessControlled": False,
    "externalSpendEnforced": False,
}
SOURCE_FIELDS = {
    "assessmentId",
    "planRevisionId",
    "approvalReviewId",
    "systemId",
    "systemRevisionId",
    "templateId",
}


def identifier(pattern, value, name):
    if not isinstance(value, str) or not pattern.fullmatch(value):
        raise ValueError("invalid " + name)
    return value


def require(condition):
    if not condition:
        raise SDKError("invalid_assessment_response")


def validated_response(validate):
    @wraps(validate)
    def read(*args, **kwargs):
        try:
            return validate(*args, **kwargs)
        except (KeyError, TypeError, ValueError, OverflowError):
            raise SDKError("invalid_assessment_response") from None

    return read


def record(value, fields=None):
    require(type(value) is dict)
    if fields is not None:
        require(set(value) == set(fields))
    return value


def timestamp(value):
    require(isinstance(value, str) and len(value) <= 40)
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        require(parsed.tzinfo is not None)
    except ValueError:
        raise SDKError("invalid_assessment_response") from None
    return parsed


def actor(value):
    record(value, {"kind", "reference"})
    require(value["kind"] in {"human", "service_key"})
    require(isinstance(value["reference"], str) and 0 < len(value["reference"]) <= 256)


def source(value):
    record(value, SOURCE_FIELDS)
    require(
        all(isinstance(item, str) and UUID.fullmatch(item) for item in value.values())
    )


def support(value):
    record(value, SUPPORT)
    require(
        all(
            type(value[key]) is type(expected) and value[key] == expected
            for key, expected in SUPPORT.items()
        )
    )


def org(value):
    require(isinstance(value, str) and 0 < len(value) <= 256)
