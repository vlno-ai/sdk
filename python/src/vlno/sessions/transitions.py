"""Monotonic recovery facts; no transition can authorize a second launch."""

import json

from .json import require
from .schema import validate


def exact(first, second):
    return json.dumps(first, sort_keys=True, allow_nan=False) == json.dumps(second, sort_keys=True, allow_nan=False)


def frozen(first, second, *keys):
    require(all(exact(first[key], second[key]) for key in keys))


def step(first, second, edges):
    require(first["state"] == second["state"] or second["state"] in edges.get(first["state"], ()))


def transition(old, new):
    validate(old)
    validate(new)
    require(new["revision"] == old["revision"] + 1)
    frozen(old, new, "schema", "id", "createdAt", "endpoint", "orgId", "source")
    a, b = old["admission"], new["admission"]
    frozen(a, b, "actor", "idempotencyKey", "command")
    step(a, b, {"prepared": ("unknown",), "unknown": ("confirmed",)})
    if a["receipt"] is not None:
        frozen(a, b, "receipt")
    a, b = old["claim"], new["claim"]
    frozen(a, b, "file", "sha256")
    step(a, b, {"not_started": ("unknown",), "unknown": ("ready", "ended"), "ready": ("ended",)})
    for key in ("actor", "case"):
        if a[key] is not None:
            frozen(a, b, key)
    a, b = old["execution"], new["execution"]
    frozen(a, b, "mode", "command")
    step(a, b, {"not_started": ("start_unknown", "external_active"),
               "start_unknown": ("running", "exit_unknown"), "running": ("exited", "exit_unknown")})
    for key in ("launchId", "pid", "exitCode", "finishedAt"):
        if a[key] is not None:
            frozen(a, b, key)
    a, b = old["capture"], new["capture"]
    step(a, b, {"not_started": ("open", "incomplete"), "open": ("complete", "incomplete"),
               "complete": ("incomplete",)})
    require(b["acknowledgedThrough"] >= a["acknowledgedThrough"])
    require(not a["gapRecorded"] or b["gapRecorded"])
    require(len(b["events"]) >= len(a["events"]))
    require(exact(a["events"], b["events"][:len(a["events"])]))
    a, b = old["finish"], new["finish"]
    step(a, b, {"not_started": ("unknown",), "unknown": ("accepted",)})
    if a["state"] != "not_started":
        frozen(a, b, "actor", "command")
    a, b = old["cancellation"], new["cancellation"]
    step(a, b, {"not_requested": ("unknown",), "unknown": ("requested",)})
    if a["actor"] is not None:
        frozen(a, b, "actor")
    if old["observation"] is not None:
        require(new["observation"] is not None)
        require(new["observation"]["value"]["version"] >= old["observation"]["value"]["version"])


def initial(document):
    validate(document)
    require(document["revision"] == 1)
    for name, state in (("admission", "prepared"), ("claim", "not_started"),
                        ("execution", "not_started"), ("capture", "not_started"),
                        ("finish", "not_started"), ("cancellation", "not_requested")):
        require(document[name]["state"] == state)
    require(document["observation"] is None and document["recovery"] is None)
