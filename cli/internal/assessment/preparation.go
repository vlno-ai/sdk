package assessment

import (
	"regexp"
	"strconv"
)

const blockers = `approval_revoked scope_expired scope_superseded source_changed
source_unavailable template_unavailable evaluation_assignment_changed material_unverifiable
worker_unavailable worker_profile_changed capacity_unavailable insufficient_authorized_time
caller_access_changed harness_profile_unsupported requested_model_unknown requested_model_unsupported`

var cost = regexp.MustCompile(`^(0|[1-9][0-9]?|100)\.[0-9]{2}$`)

// Preparation keeps readiness separate from the caller's permission to admit.
func Preparation(v map[string]any, assessmentID, revisionID string) bool {
	m, ok := object(v, "schema orgId actor source support checkedAt admissionAccess checks limits state")
	if !ok || text(m["schema"]) != "vlno.assessment-run-preparation/1" ||
		!bounded(m["orgId"]) || !actor(m["actor"]) || !support(m["support"]) {
		return false
	}
	s, ok := source(m["source"])
	if !ok || text(s["assessmentId"]) != assessmentID || text(s["planRevisionId"]) != revisionID {
		return false
	}
	if _, ok = timestamp(m["checkedAt"]); !ok || !admissionAccess(m["admissionAccess"]) {
		return false
	}
	checks, ok := m["checks"].([]any)
	if !ok || len(checks) != 4 {
		return false
	}
	state := "ready"
	for i, group := range []string{"system", "environment", "evidence", "limits"} {
		check, ok := object(checks[i], "group state blockers")
		if !ok || text(check["group"]) != group {
			return false
		}
		items, ok := check["blockers"].([]any)
		if !ok {
			return false
		}
		switch text(check["state"]) {
		case "ready":
			if len(items) != 0 {
				return false
			}
		case "blocked":
			if len(items) == 0 {
				return false
			}
			state = "blocked"
			seen := map[string]bool{}
			for _, item := range items {
				if !oneOf(item, blockers) || seen[text(item)] {
					return false
				}
				seen[text(item)] = true
			}
		default:
			return false
		}
	}
	return text(m["state"]) == state && limits(m["limits"])
}

func admissionAccess(v any) bool {
	if m, ok := object(v, "canAdmit"); ok {
		b, yes := m["canAdmit"].(bool)
		return yes && b
	}
	m, ok := object(v, "canAdmit reason")
	if !ok {
		return false
	}
	b, yes := m["canAdmit"].(bool)
	return yes && !b && oneOf(m["reason"], "grant_missing authority_unavailable")
}

func limits(v any) bool {
	m, ok := object(v, "plannedAttempts maximumAppAccessSeconds requestedMaxCostUsd externalSpendEnforced capacityReserved")
	if !ok || !integer(m["plannedAttempts"], 1, 1) || !integer(m["maximumAppAccessSeconds"], 60, 300) {
		return false
	}
	if !cost.MatchString(text(m["requestedMaxCostUsd"])) {
		return false
	}
	n, err := strconv.ParseFloat(text(m["requestedMaxCostUsd"]), 64)
	spend, a := m["externalSpendEnforced"].(bool)
	capacity, b := m["capacityReserved"].(bool)
	return err == nil && n <= 100 && a && b && !spend && !capacity
}
