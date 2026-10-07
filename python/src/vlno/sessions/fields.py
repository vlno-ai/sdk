"""Scalar and execution invariants for a versioned client journal."""

from datetime import datetime, timezone
import os
import re

from .json import integer, record, require

UUID = r"[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}"
UUID4 = r"[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}"


def match(pattern, value):
    require(type(value) is str and re.fullmatch(pattern, value) is not None)


def timestamp(value):
    match(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z", value)
    require(datetime.fromisoformat(value).isoformat(timespec="milliseconds").replace("+00:00", "Z") == value)


def now():
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def actor(value):
    record(value, "kind reference")
    require(value["kind"] in ("human", "service_key"))
    match(r"sa_[a-f0-9]{32}", value["reference"])


def command(value):
    record(value, "argv envKeys cwd")
    args, keys = value["argv"], value["envKeys"]
    require(type(args) is list and 1 <= len(args) <= 64)
    require(all(type(s) is str and "\0" not in s and len(s.encode()) <= 4096 for s in args))
    require(bool(args[0]) and sum(len(s.encode()) for s in args) <= 32768)
    require(type(keys) is list and len(keys) <= 64)
    for key in keys:
        match(r"[A-Za-z_][A-Za-z0-9_]*", key)
        upper = key.upper()
        require(not upper.startswith(("VLNO_", "CLOSED_WORLD_", "CW_", "WORKER_")) and "CLAIM" not in upper)
    require(len(set(keys)) == len(keys))
    cwd = value["cwd"]
    require(type(cwd) is str and "\0" not in cwd and len(cwd.encode()) <= 4096 and os.path.isabs(cwd))


def execution(value, claim):
    record(value, "mode state launchId command pid exitCode finishedAt")
    mode, state = value["mode"], value["state"]
    require(mode in ("supervised", "external"))
    if mode == "external":
        require(state in ("not_started", "external_active"))
        require(all(value[key] is None for key in ("command", "launchId", "pid", "exitCode", "finishedAt")))
        require(state == "not_started" or claim["case"] is not None)
        return
    command(value["command"])
    require(state in ("not_started", "start_unknown", "running", "exit_unknown", "exited"))
    if state == "not_started":
        require(all(value[key] is None for key in ("launchId", "pid", "exitCode", "finishedAt")))
        return
    require(claim["case"] is not None)
    match(UUID4, value["launchId"])
    if value["pid"] is not None:
        integer(value["pid"], 1)
    if state == "start_unknown":
        require(value["pid"] is None)
    if state in ("running", "exited"):
        require(value["pid"] is not None)
    if state == "exited":
        integer(value["exitCode"], -2147483648, 2147483647)
        timestamp(value["finishedAt"])
    else:
        require(value["exitCode"] is None and value["finishedAt"] is None)
