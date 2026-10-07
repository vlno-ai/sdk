package session

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestOutboxReservesSpaceWithoutDiscardingOrdinaryEvents(t *testing.T) {
	j := ready(t)
	c := part(j, "capture")
	c["state"] = "open"
	rows := []any{}
	appendEvent := func(index int, kind string) {
		rows = append(rows, map[string]any{"sequence": json.Number(fmt.Sprint(index)), "event": map[string]any{"id": fmt.Sprintf("%08x-0000-4000-8000-000000000000", index), "kind": kind, "occurredAt": j["updatedAt"], "data": map[string]any{"text": "retained"}}})
		c["events"] = rows
		c["nextSequence"] = json.Number(fmt.Sprint(len(rows) + 1))
	}
	for i := 1; i <= 1000; i++ {
		appendEvent(i, "stdout")
	}
	if _, e := ParseJournal(encoded(t, j)); e != nil {
		t.Fatal(e)
	}
	appendEvent(1001, "stdout")
	if _, e := ParseJournal(encoded(t, j)); e == nil {
		t.Fatal("ordinary event consumed recovery reserve")
	}
	last := part(rows[1000].(map[string]any), "event")
	last["kind"] = "gap"
	c["gapRecorded"] = true
	c["state"] = "incomplete"
	if _, e := ParseJournal(encoded(t, j)); e != nil {
		t.Fatal("reserved gap unavailable", e)
	}
	for i := 1002; i <= 1024; i++ {
		appendEvent(i, "lifecycle")
	}
	if _, e := ParseJournal(encoded(t, j)); e != nil {
		t.Fatal("reserve invalid", e)
	}
	appendEvent(1025, "lifecycle")
	if _, e := ParseJournal(encoded(t, j)); e == nil {
		t.Fatal("lifetime event cap not enforced")
	}
}
func TestCaptureCannotRegainCompletenessAfterGap(t *testing.T) {
	old := ready(t)
	c := part(old, "capture")
	c["state"] = "incomplete"
	c["gapRecorded"] = true
	c["nextSequence"] = json.Number("2")
	c["events"] = []any{map[string]any{"sequence": json.Number("1"), "event": map[string]any{"id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "kind": "gap", "occurredAt": old["updatedAt"], "data": map[string]any{"reason": "capture_incomplete"}}}}
	next := copyJournal(t, old)
	nextRevision(next)
	part(next, "capture")["state"] = "open"
	if Transition(old, next) {
		t.Fatal("incomplete capture resumed as complete-capable")
	}
}
