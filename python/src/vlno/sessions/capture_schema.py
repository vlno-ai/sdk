"""Validate transcript retention separately from execution or evaluator outcome."""

import json
from closed_world_sdk.trajectory import KINDS

from .fields import UUID4, match, timestamp
from .json import encode, integer, record, require

def capture(value, execution, claim):
    record(value, "state nextSequence acknowledgedThrough gapRecorded events")
    require(value["state"] in ("not_started", "open", "complete", "incomplete"))
    require(type(value["gapRecorded"]) is bool)
    events = value["events"]
    require(type(events) is list and len(events) <= 1024)
    integer(value["nextSequence"], len(events) + 1, len(events) + 1)
    integer(value["acknowledgedThrough"], 0, len(events))
    identifiers, total, gap = set(), 0, False
    for index, entry in enumerate(events):
        record(entry, "sequence event")
        integer(entry["sequence"], index + 1, index + 1)
        event = record(entry["event"], "id kind occurredAt data")
        match(UUID4, event["id"])
        require(event["id"] not in identifiers)
        identifiers.add(event["id"])
        require(event["kind"] in KINDS and type(event["data"]) is dict)
        timestamp(event["occurredAt"])
        size = len(encode(event))
        require(size <= 24000)
        envelope = {"claim": "A" * 43, "events": [event]}
        require(len(json.dumps(envelope, allow_nan=False).encode()) <= 65536)
        if index >= 1000 or total + size > 768 * 1024:
            require(event["kind"] in ("gap", "lifecycle") and size <= 1024)
        total += size
        gap = gap or event["kind"] == "gap"
    require(total <= 1024 * 1024 and value["gapRecorded"] == gap)
    if value["state"] == "not_started":
        require(not events and value["acknowledgedThrough"] == 0 and not gap)
        return
    require(claim["case"] is not None)
    if value["state"] == "complete":
        require(execution["mode"] == "supervised" and execution["state"] == "exited")
        require(not gap and bool(events) and value["acknowledgedThrough"] == len(events))
        last = events[-1]["event"]
        require(last["kind"] == "lifecycle")
        record(last["data"], "event exitCode streams")
        require(last["data"].get("event") == "capture_finished")
        require(type(last["data"].get("exitCode")) is int and last["data"]["exitCode"] == execution["exitCode"])
        require(last["data"].get("streams") == ["stdout", "stderr"])
