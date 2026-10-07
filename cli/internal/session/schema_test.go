package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile(filepath.Join("../../../testdata/session", name+".json"))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func journal(t *testing.T, name string) map[string]any {
	t.Helper()
	j, e := ParseJournal(fixture(t, name))
	if e != nil {
		t.Fatal(e)
	}
	return j
}
func encoded(t *testing.T, v any) []byte {
	t.Helper()
	b, e := wireJSON(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestGoldenJournals(t *testing.T) {
	for _, name := range []string{"prepared", "confirmed"} {
		j := journal(t, name)
		if e := ValidateClaim(fixture(t, "claim"), j); e != nil {
			t.Fatal(e)
		}
	}
}
func TestStrictJSON(t *testing.T) {
	invalid := []string{`{"a":1,"\u0061":2}`, `{"x":"\ud800"}`, `{"x":"\udfff"}`, `{"x":9007199254740992}`, `{"x":NaN}`, `{"x":1} {}`, `{"x":` + strings.Repeat("[", 17) + `0` + strings.Repeat("]", 17) + `}`}
	for _, raw := range invalid {
		if _, e := Decode([]byte(raw)); e == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{"x":"\ud83d\ude00"}`, `{"x":-1,"y":0.25}`, `{"x":"\\ud800"}`} {
		if _, e := Decode([]byte(raw)); e != nil {
			t.Errorf("rejected %s", raw)
		}
	}
	if _, e := Decode([]byte{'{', '"', 'x', '"', ':', '"', 255, '"', '}'}); e == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}
func TestJournalBindings(t *testing.T) {
	mutations := map[string]func(map[string]any){
		"unknown": func(j map[string]any) { j["extra"] = true },
		"receipt actor": func(j map[string]any) {
			part(part(j, "admission"), "receipt")["actor"] = map[string]any{"kind": "service_key", "reference": "sa_" + strings.Repeat("b", 32)}
		},
		"source":                func(j map[string]any) { part(j, "source")["templateId"] = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" },
		"claim":                 func(j map[string]any) { part(j, "claim")["state"] = "ready" },
		"exit":                  func(j map[string]any) { part(j, "execution")["exitCode"] = json.Number("0") },
		"ack":                   func(j map[string]any) { part(j, "capture")["acknowledgedThrough"] = json.Number("1") },
		"injected finish":       func(j map[string]any) { part(j, "finish")["state"] = "accepted" },
		"noncanonical endpoint": func(j map[string]any) { j["endpoint"] = "https://API.example.test:443/" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			j := journal(t, "confirmed")
			mutate(j)
			if _, e := ParseJournal(encoded(t, j)); e == nil {
				t.Fatal("invalid state accepted")
			}
		})
	}
}
func ready(t *testing.T) map[string]any {
	j := journal(t, "confirmed")
	c := part(j, "claim")
	c["state"] = "ready"
	c["actor"] = part(j, "admission")["actor"]
	c["case"] = map[string]any{"index": json.Number("0"), "scenario": "archive-note@1", "worldId": strings.Repeat("b", 32), "generation": json.Number("1"), "expiresAt": "2026-10-07T09:05:00.000Z"}
	return j
}
func TestCompleteCaptureRequiresExitAndFinalMarker(t *testing.T) {
	j := ready(t)
	e := part(j, "execution")
	e["state"] = "exited"
	e["launchId"] = "99999999-9999-4999-8999-999999999999"
	e["pid"] = json.Number("123")
	e["exitCode"] = json.Number("-9")
	e["finishedAt"] = j["updatedAt"]
	c := part(j, "capture")
	c["state"] = "complete"
	c["nextSequence"] = json.Number("2")
	c["acknowledgedThrough"] = json.Number("1")
	data := map[string]any{"event": "capture_finished", "exitCode": json.Number("-9"), "streams": []any{"stdout", "stderr"}}
	c["events"] = []any{map[string]any{"sequence": json.Number("1"), "event": map[string]any{"id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "kind": "lifecycle", "occurredAt": j["updatedAt"], "data": data}}}
	if _, err := ParseJournal(encoded(t, j)); err != nil {
		t.Fatal(err)
	}
	data["event"] = "arbitrary"
	if _, err := ParseJournal(encoded(t, j)); err == nil {
		t.Fatal("no completion marker")
	}
}
func TestOrigin(t *testing.T) {
	data, e := Decode(fixture(t, "origins"))
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range data["valid"].([]any) {
		row := v.(map[string]any)
		got, e := Origin(text(row["input"]))
		if e != nil || got != row["origin"] {
			t.Fatalf("%q: %q %v", row["input"], got, e)
		}
	}
	for _, v := range data["invalid"].([]any) {
		if _, e := Origin(text(v)); e == nil {
			t.Errorf("accepted %q", v)
		}
	}
}
