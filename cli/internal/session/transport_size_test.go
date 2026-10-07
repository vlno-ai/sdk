package session

import (
	"encoding/json"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestPortableEventEnvelopeLimit(t *testing.T) {
	event, e := Decode(fixture(t, "invalid-event-envelope"))
	if e != nil {
		t.Fatal(e)
	}
	compact := encoded(t, event)
	if len(compact) > 24000 || PythonTransportSize(event) <= 65536 {
		t.Fatal("fixture does not exercise separate bounds")
	}
	j := ready(t)
	c := part(j, "capture")
	c["state"] = "open"
	c["nextSequence"] = json.Number("2")
	c["events"] = []any{map[string]any{"sequence": json.Number("1"), "event": event}}
	if _, e = ParseJournal(encoded(t, j)); e == nil {
		t.Fatal("Python-unrecoverable event persisted")
	}
	part(event, "data")["text"] = strings.Repeat("🌏", 2048)
	if _, e = ParseJournal(encoded(t, j)); e != nil {
		t.Fatal("normal recorder chunk rejected", e)
	}
}
func TestPythonDefaultTransportEncodingParity(t *testing.T) {
	event := map[string]any{"id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "kind": "stdout", "occurredAt": "2026-10-07T09:00:00.000Z", "data": map[string]any{"text": "🌏中<&\u2028\u2029\x7f\n", "numbers": []any{json.Number("-9"), json.Number("-0.0"), json.Number("1e-5"), json.Number("1e15"), json.Number("1e-4"), json.Number("0.3333333333333333")}}}
	cmd := exec.Command("python3", "-c", `import json,sys
v=json.load(sys.stdin)
print(len(json.dumps({'claim':'A'*43,'events':[v]}).encode()))`)
	cmd.Stdin = strings.NewReader(string(encoded(t, event)))
	out, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	want, e := strconv.Atoi(strings.TrimSpace(string(out)))
	if e != nil || PythonTransportSize(event) != want {
		t.Fatalf("Go %v Python %v: %v", PythonTransportSize(event), want, e)
	}
}

func TestCompactFloatingPointBudgetMatchesPython(t *testing.T) {
	event := map[string]any{"values": []any{json.Number("1e15"), json.Number("1e-6"), json.Number("-0.0"), json.Number("1.23456789123456789")}, "text": "<🌏\u2028"}
	cmd := exec.Command("python3", "-c", `import json,sys
v=json.load(sys.stdin)
s=json.dumps(v,ensure_ascii=False,separators=(',',':')).replace('\u2028','\\u2028').replace('\u2029','\\u2029')
print(len(s.encode()))`)
	cmd.Stdin = strings.NewReader(string(encoded(t, event)))
	out, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	want, e := strconv.Atoi(strings.TrimSpace(string(out)))
	if e != nil || len(encoded(t, event)) != want {
		t.Fatalf("Go compact %v Python %v: %v", len(encoded(t, event)), want, e)
	}
}

func TestNumericSpellingsRemainStableAcrossRecovery(t *testing.T) {
	a, e := Decode([]byte(`{"integer":-0,"float":1e15}`))
	if e != nil {
		t.Fatal(e)
	}
	b, e := Decode([]byte(`{"integer":0,"float":1000000000000000.0}`))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("equivalent Python-decoded values diverged")
	}
}
