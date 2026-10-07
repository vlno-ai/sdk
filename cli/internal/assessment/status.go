package assessment

import "time"

// Status validates current state independently of the immutable admission receipt.
func Status(v map[string]any, id string) bool {
	m, ok := object(v, `schema orgId id source admittedBy support phase version admittedAt claimBefore
accessEndsAt lastConfirmedAt synchronization cleanup outcome evidence actions`)
	if !ok || text(m["schema"]) != "vlno.assessment-run/1" || text(m["id"]) != id || !RunID.MatchString(id) ||
		!bounded(m["orgId"]) || !actor(m["admittedBy"]) || !support(m["support"]) {
		return false
	}
	if _, ok = source(m["source"]); !ok {
		return false
	}
	if !oneOf(m["phase"], "awaiting_harness preparing running collecting_evidence finished cancel_requested cancelled interrupted expired") ||
		!integer(m["version"], 1, 9007199254740991) || !oneOf(m["synchronization"], "confirmed reconciling unavailable") ||
		!oneOf(m["cleanup"], "not_allocated pending confirmed failed") {
		return false
	}
	admitted, a := timestamp(m["admittedAt"])
	claim, b := timestamp(m["claimBefore"])
	_, c := timestamp(m["lastConfirmedAt"])
	if !a || !b || !c || !claim.After(admitted) || claim.Sub(admitted) > 10*time.Minute {
		return false
	}
	if m["accessEndsAt"] != nil {
		end, ok := timestamp(m["accessEndsAt"])
		if !ok || !end.After(admitted) {
			return false
		}
	}
	actions, ok := object(m["actions"], "canClaim canCancel")
	return ok && boolean(actions["canClaim"]) && boolean(actions["canCancel"]) && evidence(m["evidence"]) && outcome(m["outcome"])
}

func evidence(v any) bool {
	m, ok := object(v, "state historyAvailable observationsAvailable")
	if !ok || !boolean(m["historyAvailable"]) || !boolean(m["observationsAvailable"]) {
		return false
	}
	history, observations := m["historyAvailable"].(bool), m["observationsAvailable"].(bool)
	switch text(m["state"]) {
	case "pending":
		return !history && !observations
	case "available":
		return history
	case "incomplete", "unavailable", "removed":
		return !observations
	default:
		return false
	}
}

func outcome(v any) bool {
	if v == nil {
		return true
	}
	m, ok := object(v, "status assurance scope semanticCheck observationAuthority")
	if !ok || (text(m["semanticCheck"]) == "verified_from_sealed_worker_receipts" && !oneOf(m["status"], "passed failed")) {
		return false
	}
	return oneOf(m["status"], "passed failed inconclusive invalid error not_run") &&
		text(m["assurance"]) == "trusted_worker" && text(m["observationAuthority"]) == "trusted_worker" &&
		text(m["scope"]) == "final_target_archived_and_peer_final_state" &&
		oneOf(m["semanticCheck"], "not_available verified_from_sealed_worker_receipts")
}
