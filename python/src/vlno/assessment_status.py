"""Conservative validation of current approved-run status."""

import copy

from .assessment_fields import (
    actor,
    org,
    record,
    require,
    source,
    support,
    timestamp,
    validated_response,
)

PHASES = {
    "awaiting_harness",
    "preparing",
    "running",
    "collecting_evidence",
    "finished",
    "cancel_requested",
    "cancelled",
    "interrupted",
    "expired",
}


@validated_response
def assessment_status(value, run_id):
    record(
        value,
        {
            "schema",
            "orgId",
            "id",
            "source",
            "admittedBy",
            "support",
            "phase",
            "version",
            "admittedAt",
            "claimBefore",
            "accessEndsAt",
            "lastConfirmedAt",
            "synchronization",
            "cleanup",
            "outcome",
            "evidence",
            "actions",
        },
    )
    require(value["schema"] == "vlno.assessment-run/1" and value["id"] == run_id)
    org(value["orgId"])
    actor(value["admittedBy"])
    source(value["source"])
    support(value["support"])
    require(
        value["phase"] in PHASES
        and type(value["version"]) is int
        and value["version"] > 0
    )
    require(value["synchronization"] in {"confirmed", "reconciling", "unavailable"})
    require(value["cleanup"] in {"not_allocated", "pending", "confirmed", "failed"})
    admitted = timestamp(value["admittedAt"])
    window = timestamp(value["claimBefore"]) - admitted
    require(0 < window.total_seconds() <= 600)
    timestamp(value["lastConfirmedAt"])
    if value["accessEndsAt"] is not None:
        require(timestamp(value["accessEndsAt"]) > admitted)
    actions = record(value["actions"], {"canClaim", "canCancel"})
    require(all(type(item) is bool for item in actions.values()))
    evidence = record(
        value["evidence"], {"state", "historyAvailable", "observationsAvailable"}
    )
    require(
        type(evidence["historyAvailable"]) is bool
        and type(evidence["observationsAvailable"]) is bool
    )
    if evidence["state"] == "available":
        require(evidence["historyAvailable"] is True)
    elif evidence["state"] == "pending":
        require(
            evidence["historyAvailable"] is False
            and evidence["observationsAvailable"] is False
        )
    else:
        require(
            evidence["state"] in {"incomplete", "unavailable", "removed"}
            and evidence["observationsAvailable"] is False
        )
    if value["outcome"] is not None:
        outcome = record(
            value["outcome"],
            {"status", "assurance", "scope", "semanticCheck", "observationAuthority"},
        )
        require(
            outcome["status"]
            in {"passed", "failed", "inconclusive", "invalid", "error", "not_run"}
        )
        require(
            outcome["assurance"] == outcome["observationAuthority"] == "trusted_worker"
        )
        require(outcome["scope"] == "final_target_archived_and_peer_final_state")
        require(
            outcome["semanticCheck"]
            in {"not_available", "verified_from_sealed_worker_receipts"}
        )
        if outcome["semanticCheck"] == "verified_from_sealed_worker_receipts":
            require(outcome["status"] in {"passed", "failed"})
    return copy.deepcopy(value)
