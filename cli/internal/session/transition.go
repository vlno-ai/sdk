package session

import "reflect"

// Transition rejects rebasing an intent or weakening a previously durable fact.
func Transition(old, next map[string]any) bool {
	if !validJournal(old) || !validJournal(next) || number(next["revision"]) != number(old["revision"])+1 {
		return false
	}
	for _, key := range []string{"schema", "id", "createdAt", "endpoint", "orgId", "source"} {
		if !reflect.DeepEqual(old[key], next[key]) {
			return false
		}
	}
	a, b := part(old, "admission"), part(next, "admission")
	if !frozen(a, b, "actor", "idempotencyKey", "command") || !step(a, b, map[string]string{"prepared": "unknown", "unknown": "confirmed"}) || a["receipt"] != nil && !reflect.DeepEqual(a["receipt"], b["receipt"]) {
		return false
	}
	a, b = part(old, "claim"), part(next, "claim")
	if !frozen(a, b, "file", "sha256") || !step(a, b, map[string]string{"not_started": "unknown", "unknown": "ready ended", "ready": "ended"}) || a["actor"] != nil && !reflect.DeepEqual(a["actor"], b["actor"]) || a["case"] != nil && !reflect.DeepEqual(a["case"], b["case"]) {
		return false
	}
	a, b = part(old, "execution"), part(next, "execution")
	if !frozen(a, b, "mode", "command") || !step(a, b, map[string]string{"not_started": "start_unknown external_active", "start_unknown": "running exit_unknown", "running": "exited exit_unknown"}) {
		return false
	}
	for _, k := range []string{"launchId", "pid", "exitCode", "finishedAt"} {
		if a[k] != nil && !reflect.DeepEqual(a[k], b[k]) {
			return false
		}
	}
	a, b = part(old, "capture"), part(next, "capture")
	if !step(a, b, map[string]string{"not_started": "open incomplete", "open": "complete incomplete", "complete": "incomplete"}) || number(b["acknowledgedThrough"]) < number(a["acknowledgedThrough"]) || a["gapRecorded"] == true && b["gapRecorded"] != true {
		return false
	}
	previous, following := a["events"].([]any), b["events"].([]any)
	if len(following) < len(previous) {
		return false
	}
	for i, e := range previous {
		if !reflect.DeepEqual(e, following[i]) {
			return false
		}
	}
	a, b = part(old, "finish"), part(next, "finish")
	if !step(a, b, map[string]string{"not_started": "unknown", "unknown": "accepted"}) || a["state"] != "not_started" && !frozen(a, b, "actor", "command") {
		return false
	}
	a, b = part(old, "cancellation"), part(next, "cancellation")
	if !step(a, b, map[string]string{"not_requested": "unknown", "unknown": "requested"}) || a["actor"] != nil && !reflect.DeepEqual(a["actor"], b["actor"]) {
		return false
	}
	if old["observation"] != nil {
		if next["observation"] == nil {
			return false
		}
		before := part(part(old, "observation"), "value")
		after := part(part(next, "observation"), "value")
		if number(after["version"]) < number(before["version"]) {
			return false
		}
	}
	return true
}
func part(m map[string]any, key string) map[string]any { v, _ := m[key].(map[string]any); return v }
func frozen(a, b map[string]any, keys ...string) bool {
	for _, k := range keys {
		if !reflect.DeepEqual(a[k], b[k]) {
			return false
		}
	}
	return true
}
func step(a, b map[string]any, allowed map[string]string) bool {
	return a["state"] == b["state"] || one(b["state"], allowed[text(a["state"])])
}
