"""Public readiness and exact immutable run-admission receipts."""

import copy
import re
from decimal import Decimal

from .assessment_fields import (
    actor,
    org,
    record,
    require,
    source,
    support,
    timestamp,
    RUN,
    validated_response,
)

BLOCKERS = {
    "approval_revoked",
    "scope_expired",
    "scope_superseded",
    "source_changed",
    "source_unavailable",
    "template_unavailable",
    "evaluation_assignment_changed",
    "material_unverifiable",
    "worker_unavailable",
    "worker_profile_changed",
    "capacity_unavailable",
    "insufficient_authorized_time",
    "caller_access_changed",
    "harness_profile_unsupported",
    "requested_model_unknown",
    "requested_model_unsupported",
}


@validated_response
def preparation(value, assessment_id, revision_id):
    record(
        value,
        {
            "schema",
            "orgId",
            "actor",
            "source",
            "support",
            "checkedAt",
            "admissionAccess",
            "checks",
            "limits",
            "state",
        },
    )
    require(value["schema"] == "vlno.assessment-run-preparation/1")
    org(value["orgId"])
    actor(value["actor"])
    source(value["source"])
    support(value["support"])
    timestamp(value["checkedAt"])
    require(
        value["source"]["assessmentId"] == assessment_id
        and value["source"]["planRevisionId"] == revision_id
    )
    access = record(value["admissionAccess"])
    if access.get("canAdmit") is True:
        record(access, {"canAdmit"})
    else:
        record(access, {"canAdmit", "reason"})
        require(
            access["canAdmit"] is False
            and access["reason"] in {"grant_missing", "authority_unavailable"}
        )
    checks = value["checks"]
    require(type(checks) is list and len(checks) == 4)
    for check, group in zip(checks, ("system", "environment", "evidence", "limits")):
        record(check, {"group", "state", "blockers"})
        require(check["group"] == group and type(check["blockers"]) is list)
        if check["state"] == "ready":
            require(check["blockers"] == [])
        else:
            require(
                check["state"] == "blocked"
                and 0 < len(check["blockers"]) <= len(BLOCKERS)
            )
            require(
                all(
                    type(item) is str and item in BLOCKERS for item in check["blockers"]
                )
            )
            require(len(set(check["blockers"])) == len(check["blockers"]))
    expected = (
        "ready" if all(check["state"] == "ready" for check in checks) else "blocked"
    )
    require(value["state"] == expected)
    limits = record(
        value["limits"],
        {
            "plannedAttempts",
            "maximumAppAccessSeconds",
            "requestedMaxCostUsd",
            "externalSpendEnforced",
            "capacityReserved",
        },
    )
    require(type(limits["plannedAttempts"]) is int and limits["plannedAttempts"] == 1)
    require(
        type(limits["maximumAppAccessSeconds"]) is int
        and 60 <= limits["maximumAppAccessSeconds"] <= 300
    )
    require(
        type(limits["requestedMaxCostUsd"]) is str
        and re.fullmatch(
            r"(?:0|[1-9][0-9]?|100)\.[0-9]{2}", limits["requestedMaxCostUsd"]
        )
    )
    require(Decimal(limits["requestedMaxCostUsd"]) <= 100)
    require(
        limits["externalSpendEnforced"] is False and limits["capacityReserved"] is False
    )
    return copy.deepcopy(value)


@validated_response
def admission(value, assessment_id, command, key):
    record(
        value,
        {
            "schema",
            "orgId",
            "actor",
            "idempotencyKey",
            "submitted",
            "source",
            "runId",
            "admittedAt",
            "claimBefore",
            "reservedAttempts",
            "executionStarted",
        },
    )
    require(value["schema"] == "vlno.assessment-run-admission/1")
    org(value["orgId"])
    actor(value["actor"])
    source(value["source"])
    require(value["idempotencyKey"] == key and value["submitted"] == command)
    require(value["source"]["assessmentId"] == assessment_id)
    require(
        all(value["source"][name] == expected for name, expected in command.items())
    )
    require(type(value["runId"]) is str and RUN.fullmatch(value["runId"]))
    window = timestamp(value["claimBefore"]) - timestamp(value["admittedAt"])
    require(0 < window.total_seconds() <= 600)
    require(type(value["reservedAttempts"]) is int and value["reservedAttempts"] == 1)
    require(value["executionStarted"] is False)
    return copy.deepcopy(value)
