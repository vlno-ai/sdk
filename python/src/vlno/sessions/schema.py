"""Validate immutable intent and independent lifecycle facts in client journals."""

import base64
import hashlib

from closed_world_sdk import SDKError
from ..assessment_preparation import admission
from ..assessment_status import assessment_status
from .capture_schema import capture
from .fields import UUID, UUID4, actor, execution, match, timestamp
from .identity import endpoint_origin
from .json import decode, encode, integer, record, require, MAXIMUM
from .files import SessionFileError


def claim_record(value):
    record(value, "file sha256 state actor case")
    require(value["file"] == "claim.json")
    match(r"[a-f0-9]{64}", value["sha256"])
    require(value["state"] in ("not_started", "unknown", "ready", "ended"))
    if value["state"] == "not_started":
        require(value["actor"] is None)
    else:
        actor(value["actor"])
    case = value["case"]
    if case is None:
        require(value["state"] != "ready")
        return
    require(value["state"] in ("ready", "ended"))
    record(case, "index scenario worldId generation expiresAt")
    integer(case["index"], 0, 0)
    integer(case["generation"], 1, 1)
    require(case["scenario"] == "archive-note@1")
    match(r"[a-f0-9]{32}", case["worldId"])
    timestamp(case["expiresAt"])


def _validate(value):
    record(value, "schema id revision createdAt updatedAt endpoint orgId source admission claim execution capture finish cancellation observation recovery")
    require(value["schema"] == "vlno.client-session/1")
    match(UUID4, value["id"])
    integer(value["revision"], 1)
    timestamp(value["createdAt"])
    timestamp(value["updatedAt"])
    require(endpoint_origin(value["endpoint"]) == value["endpoint"])
    require(type(value["orgId"]) is str and 1 <= len(value["orgId"]) <= 200)
    source = record(value["source"], "assessmentId planRevisionId approvalReviewId systemId systemRevisionId templateId")
    for ident in source.values():
        match(UUID, ident)
    accepted = record(value["admission"], "state actor idempotencyKey command receipt")
    require(accepted["state"] in ("prepared", "unknown", "confirmed"))
    actor(accepted["actor"])
    match(UUID4, accepted["idempotencyKey"])
    command = record(accepted["command"], "planRevisionId approvalReviewId systemRevisionId")
    require(command == {key: source[key] for key in command})
    receipt = accepted["receipt"]
    if accepted["state"] == "confirmed":
        admission(receipt, source["assessmentId"], command, accepted["idempotencyKey"])
        require(receipt["orgId"] == value["orgId"] and receipt["actor"] == accepted["actor"] and receipt["source"] == source)
    else:
        require(receipt is None)
    claim = value["claim"]
    claim_record(claim)
    require(claim["state"] == "not_started" or receipt is not None)
    execution(value["execution"], claim)
    capture(value["capture"], value["execution"], claim)
    finish = record(value["finish"], "state actor command")
    require(finish["state"] in ("not_started", "unknown", "accepted"))
    if finish["state"] == "not_started":
        require(finish["actor"] is None and finish["command"] is None)
    else:
        require(claim["case"] is not None and finish["actor"] == claim["actor"])
        actor(finish["actor"])
        record(finish["command"], "case_index agent_status")
        integer(finish["command"]["case_index"], 0, 0)
        require(finish["command"]["agent_status"] in ("completed", "error", "timeout"))
    cancel = record(value["cancellation"], "state actor")
    require(cancel["state"] in ("not_requested", "unknown", "requested"))
    if cancel["state"] == "not_requested":
        require(cancel["actor"] is None)
    else:
        require(receipt is not None)
        actor(cancel["actor"])
    if value["observation"] is not None:
        observation = record(value["observation"], "checkedAt value")
        timestamp(observation["checkedAt"])
        require(receipt is not None)
        current = assessment_status(observation["value"], receipt["runId"])
        require(current["orgId"] == value["orgId"] and current["source"] == source and current["admittedBy"] == accepted["actor"])
    if value["recovery"] is not None:
        recovery = record(value["recovery"], "code occurredAt")
        timestamp(recovery["occurredAt"])
        require(recovery["code"] in ("admission_unknown", "assignment_unknown", "launch_unknown", "exit_unknown",
                "capture_incomplete", "finish_unknown", "cancel_unknown", "authority_changed", "source_changed",
                "connection_expired", "service_unavailable", "outbox_full"))


def validate(value):
    try:
        require(len(encode(value)) <= MAXIMUM)
        _validate(value)
        return value
    except (ValueError, TypeError, KeyError, SDKError, OverflowError):
        raise SessionFileError("session_corrupt") from None


def parse(raw):
    return validate(decode(raw))


def validate_claim(raw, journal):
    require(len(raw) <= 1024)
    value = record(decode(raw), "schema sessionId claim")
    require(value["schema"] == "vlno.client-session-claim/1" and value["sessionId"] == journal["id"])
    claim = value["claim"]
    match(r"[A-Za-z0-9_-]{43}", claim)
    decoded = base64.urlsafe_b64decode(claim + "=")
    require(len(decoded) == 32 and base64.urlsafe_b64encode(decoded).decode().rstrip("=") == claim)
    require(hashlib.sha256(claim.encode()).hexdigest() == journal["claim"]["sha256"])
    return claim
