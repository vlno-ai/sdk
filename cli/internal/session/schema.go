package session

import (
	"github.com/vlno-ai/sdk/cli/internal/assessment"
	"reflect"
)

// ParseJournal validates both exact public DTOs and local cross-record bindings.
func ParseJournal(data []byte) (map[string]any, error) {
	m, e := Decode(data)
	if e != nil || !validJournal(m) {
		return nil, ErrCorrupt
	}
	return m, nil
}
func validJournal(v map[string]any) bool {
	m, ok := object(v, "schema id revision createdAt updatedAt endpoint orgId source admission claim execution capture finish cancellation observation recovery")
	if !ok || text(m["schema"]) != "vlno.client-session/1" || !uuid4.MatchString(text(m["id"])) || !integer(m["revision"], 1, 9007199254740991) || !timestamp(m["createdAt"]) || !timestamp(m["updatedAt"]) || !bounded(m["orgId"], 200) {
		return false
	}
	endpoint, e := Origin(text(m["endpoint"]))
	if e != nil || endpoint != m["endpoint"] {
		return false
	}
	source, ok := object(m["source"], "assessmentId planRevisionId approvalReviewId systemId systemRevisionId templateId")
	if !ok {
		return false
	}
	for _, v := range source {
		if !uuid.MatchString(text(v)) {
			return false
		}
	}
	a, ok := object(m["admission"], "state actor idempotencyKey command receipt")
	if !ok || !one(a["state"], "prepared unknown confirmed") || !actor(a["actor"]) || !uuid4.MatchString(text(a["idempotencyKey"])) {
		return false
	}
	command, ok := object(a["command"], "planRevisionId approvalReviewId systemRevisionId")
	if !ok {
		return false
	}
	for k, v := range command {
		if v != source[k] {
			return false
		}
	}
	receipt, hasReceipt := a["receipt"].(map[string]any)
	if text(a["state"]) == "confirmed" {
		if !hasReceipt || !assessment.Admission(receipt, text(source["assessmentId"]), command, text(a["idempotencyKey"])) || receipt["orgId"] != m["orgId"] || !reflect.DeepEqual(receipt["actor"], a["actor"]) || !reflect.DeepEqual(receipt["source"], source) {
			return false
		}
	} else if a["receipt"] != nil {
		return false
	}
	c, ok := validClaim(m["claim"])
	if !ok || text(c["state"]) != "not_started" && !hasReceipt {
		return false
	}
	if !validExecution(m["execution"], c) || !validCapture(m["capture"], m["execution"], c) || !validFinish(m["finish"], c) || !validCancel(m["cancellation"], hasReceipt) {
		return false
	}
	if m["observation"] != nil {
		o, ok := object(m["observation"], "checkedAt value")
		if !ok || !timestamp(o["checkedAt"]) || !hasReceipt {
			return false
		}
		value, ok := o["value"].(map[string]any)
		if !ok || !assessment.Status(value, text(receipt["runId"])) || value["orgId"] != m["orgId"] || !reflect.DeepEqual(value["source"], source) || !reflect.DeepEqual(value["admittedBy"], a["actor"]) {
			return false
		}
	}
	if m["recovery"] != nil {
		r, ok := object(m["recovery"], "code occurredAt")
		if !ok || !timestamp(r["occurredAt"]) || !one(r["code"], "admission_unknown assignment_unknown launch_unknown exit_unknown capture_incomplete finish_unknown cancel_unknown authority_changed source_changed connection_expired service_unavailable outbox_full") {
			return false
		}
	}
	return true
}
func validClaim(v any) (map[string]any, bool) {
	m, ok := object(v, "file sha256 state actor case")
	if !ok || m["file"] != "claim.json" || !digest.MatchString(text(m["sha256"])) || !one(m["state"], "not_started unknown ready ended") {
		return nil, false
	}
	started := m["state"] != "not_started"
	if started != actor(m["actor"]) || !started && m["actor"] != nil {
		return nil, false
	}
	if m["case"] == nil {
		return m, m["state"] != "ready"
	}
	c, ok := object(m["case"], "index scenario worldId generation expiresAt")
	return m, ok && one(m["state"], "ready ended") && integer(c["index"], 0, 0) && c["scenario"] == "archive-note@1" && hex32.MatchString(text(c["worldId"])) && integer(c["generation"], 1, 1) && timestamp(c["expiresAt"])
}
func validFinish(v any, c map[string]any) bool {
	m, ok := object(v, "state actor command")
	if !ok || !one(m["state"], "not_started unknown accepted") {
		return false
	}
	if m["state"] == "not_started" {
		return m["actor"] == nil && m["command"] == nil
	}
	command, ok := object(m["command"], "case_index agent_status")
	return ok && c["case"] != nil && actor(m["actor"]) && reflect.DeepEqual(m["actor"], c["actor"]) && integer(command["case_index"], 0, 0) && one(command["agent_status"], "completed error timeout")
}
func validCancel(v any, admitted bool) bool {
	m, ok := object(v, "state actor")
	if !ok || !one(m["state"], "not_requested unknown requested") {
		return false
	}
	if m["state"] == "not_requested" {
		return m["actor"] == nil
	}
	return admitted && actor(m["actor"])
}
