package assessment

import "time"

// Admission validates the immutable receipt against the exact submitted command.
func Admission(v map[string]any, assessmentID string, command map[string]any, key string) bool {
	m, ok := object(v, "schema orgId actor idempotencyKey submitted source runId admittedAt claimBefore reservedAttempts executionStarted")
	if !ok || text(m["schema"]) != "vlno.assessment-run-admission/1" || !bounded(m["orgId"]) ||
		!actor(m["actor"]) || text(m["idempotencyKey"]) != key || !RunID.MatchString(text(m["runId"])) {
		return false
	}
	s, ok := source(m["source"])
	if !ok || text(s["assessmentId"]) != assessmentID {
		return false
	}
	submitted, ok := object(m["submitted"], "planRevisionId approvalReviewId systemRevisionId")
	if !ok || len(command) != len(submitted) {
		return false
	}
	for name, value := range command {
		if !UUID.MatchString(text(value)) || text(submitted[name]) != text(value) || text(s[name]) != text(value) {
			return false
		}
	}
	admitted, a := timestamp(m["admittedAt"])
	claim, b := timestamp(m["claimBefore"])
	started, c := m["executionStarted"].(bool)
	window := claim.Sub(admitted)
	return a && b && c && !started && window > 0 && window <= 10*time.Minute && integer(m["reservedAttempts"], 1, 1)
}
