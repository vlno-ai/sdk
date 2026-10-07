package session

import (
	"bytes"
	"encoding/json"
	"reflect"
)

func validCapture(v, execution any, claim map[string]any) bool {
	m, ok := object(v, "state nextSequence acknowledgedThrough gapRecorded events")
	if !ok || !one(m["state"], "not_started open complete incomplete") || !boolean(m["gapRecorded"]) {
		return false
	}
	events, ok := m["events"].([]any)
	if !ok || len(events) > 1024 || !integer(m["nextSequence"], int64(len(events)+1), int64(len(events)+1)) || !integer(m["acknowledgedThrough"], 0, int64(len(events))) {
		return false
	}
	ids := map[string]bool{}
	total := 0
	gap := false
	for i, v := range events {
		e, ok := object(v, "sequence event")
		if !ok || !integer(e["sequence"], int64(i+1), int64(i+1)) {
			return false
		}
		event, ok := object(e["event"], "id kind occurredAt data")
		if !ok || !uuid4.MatchString(text(event["id"])) || ids[text(event["id"])] || !timestamp(event["occurredAt"]) || !one(event["kind"], "message tool_call tool_result shell_command shell_output stdout stderr lifecycle gap") {
			return false
		}
		if _, ok := event["data"].(map[string]any); !ok {
			return false
		}
		b, err := wireJSON(event)
		if err != nil || len(b) > 24000 || PythonTransportSize(event) > 65536 {
			return false
		}
		if i >= 1000 || total+len(b) > 768*1024 {
			if !one(event["kind"], "gap lifecycle") || len(b) > 1024 {
				return false
			}
		}
		total += len(b)
		ids[text(event["id"])] = true
		if event["kind"] == "gap" {
			gap = true
		}
	}
	if total > 1024*1024 || m["gapRecorded"] != gap {
		return false
	}
	if m["state"] == "not_started" {
		return len(events) == 0 && number(m["acknowledgedThrough"]) == 0 && !gap
	}
	if claim["case"] == nil {
		return false
	}
	if m["state"] == "complete" {
		e := execution.(map[string]any)
		if len(events) == 0 {
			return false
		}
		last := part(events[len(events)-1].(map[string]any), "event")
		data := part(last, "data")
		if last["kind"] != "lifecycle" || !reflect.DeepEqual(data, map[string]any{"event": "capture_finished", "exitCode": e["exitCode"], "streams": []any{"stdout", "stderr"}}) {
			return false
		}
		return e["mode"] == "supervised" && e["state"] == "exited" && !gap && len(events) > 0 && number(m["acknowledgedThrough"]) == int64(len(events))
	}
	return true
}

// Compact UTF-8 representation, without HTML escaping, for common byte limits.
func wireJSON(v any) ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if e := enc.Encode(portableNumbers(v)); e != nil {
		return nil, e
	}
	b := out.Bytes()
	return b[:len(b)-1], nil
}
