package session

import (
	"encoding/json"
	"strconv"
	"testing"
)

func copyJournal(t *testing.T, j map[string]any) map[string]any {
	t.Helper()
	v, e := Decode(encoded(t, j))
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func nextRevision(j map[string]any) {
	j["revision"] = json.Number(strconv.FormatInt(number(j["revision"])+1, 10))
}
func TestAdmissionMustPersistUnknownBeforeConfirmation(t *testing.T) {
	old := journal(t, "prepared")
	next := journal(t, "confirmed")
	if Transition(old, next) {
		t.Fatal("skipped durable intent")
	}
	pending := copyJournal(t, old)
	nextRevision(pending)
	part(pending, "admission")["state"] = "unknown"
	if !Transition(old, pending) {
		t.Fatal("intent rejected")
	}
	nextRevision(next)
	if !Transition(pending, next) {
		t.Fatal("confirmation rejected")
	}
	changed := copyJournal(t, next)
	nextRevision(changed)
	part(changed, "admission")["idempotencyKey"] = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if Transition(next, changed) {
		t.Fatal("rebased key")
	}
}
func TestNoRespawnOrProcessIdentityRewrite(t *testing.T) {
	before := ready(t)
	x := part(before, "execution")
	x["state"] = "start_unknown"
	x["launchId"] = "99999999-9999-4999-8999-999999999999"
	next := copyJournal(t, before)
	nextRevision(next)
	part(next, "execution")["state"] = "exit_unknown"
	if !Transition(before, next) {
		t.Fatal("uncertainty rejected")
	}
	replay := copyJournal(t, next)
	nextRevision(replay)
	part(replay, "execution")["state"] = "running"
	part(replay, "execution")["pid"] = json.Number("1")
	if Transition(next, replay) {
		t.Fatal("respawn accepted")
	}
}
func TestFinishAndOutboxRemainImmutable(t *testing.T) {
	old := ready(t)
	f := part(old, "finish")
	f["state"] = "unknown"
	f["actor"] = part(old, "claim")["actor"]
	f["command"] = map[string]any{"case_index": json.Number("0"), "agent_status": "error"}
	next := copyJournal(t, old)
	nextRevision(next)
	part(next, "finish")["state"] = "accepted"
	if !Transition(old, next) {
		t.Fatal("same finish rejected")
	}
	part(part(next, "finish"), "command")["agent_status"] = "completed"
	if Transition(old, next) {
		t.Fatal("finish status rebased")
	}
	c := part(old, "capture")
	c["state"] = "open"
	c["nextSequence"] = json.Number("2")
	c["events"] = []any{map[string]any{"sequence": json.Number("1"), "event": map[string]any{"id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "kind": "stdout", "occurredAt": old["updatedAt"], "data": map[string]any{"text": "original"}}}}
	next = copyJournal(t, old)
	nextRevision(next)
	part(next, "capture")["acknowledgedThrough"] = json.Number("1")
	if !Transition(old, next) {
		t.Fatal("ack rejected")
	}
	events := part(next, "capture")["events"].([]any)
	part(part(events[0].(map[string]any), "event"), "data")["text"] = "changed"
	if Transition(old, next) {
		t.Fatal("event rewrite accepted")
	}
}
