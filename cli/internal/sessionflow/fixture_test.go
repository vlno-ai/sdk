package sessionflow

import (
	"context"
	"encoding/json"
	"github.com/vlno-ai/sdk/cli/internal/session"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeClient struct {
	call func(string, string, any, string) (map[string]any, error)
}

func (f fakeClient) Request(_ context.Context, m, p string, b any, k string) (map[string]any, error) {
	return f.call(m, p, b, k)
}
func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	b, e := os.ReadFile(filepath.Join("../../../testdata/session", name+".json"))
	if e != nil {
		t.Fatal(e)
	}
	v, e := session.Decode(b)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func encoded(t *testing.T, v any) []byte {
	t.Helper()
	b, e := session.Encode(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func protocol(t *testing.T) *Protocol {
	t.Helper()
	s, e := session.Create(filepath.Join(t.TempDir(), "session"), encoded(t, fixture(t, "prepared")), encoded(t, fixture(t, "claim")))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return &Protocol{

		Store: s,

		Endpoint: "https://api.example.test",

		Bindings: Bindings{
			ValidRun: func(v map[string]any, id string) bool { return v["id"] == id },
			Terminal: func(v map[string]any) bool { return v["state"] == "cancelled" },
		},
	}
}
func who(t *testing.T) map[string]any {
	return map[string]any{

		"schema": "vlno.assessment-run-access/1",

		"orgId": "org-fixture",

		"actor": object(fixture(t, "prepared"), "admission")["actor"],
	}
}
func receipt(t *testing.T) map[string]any {
	return object(object(fixture(t, "confirmed"), "admission"), "receipt")
}
func confirmed(t *testing.T, p *Protocol) {
	t.Helper()
	p.Client = fakeClient{func(m, path string, b any, k string) (map[string]any, error) {
		if m == "GET" {
			return who(t), nil
		}
		return receipt(t), nil
	}}
	if e := p.Admit(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func claimed(t *testing.T, p *Protocol) {
	confirmed(t, p)
	e := p.Store.Update(func(j map[string]any) error {
		c := object(j, "claim")
		c["state"] = "unknown"
		c["actor"] = object(j, "admission")["actor"]
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	e = p.Store.Update(func(j map[string]any) error {
		c := object(j, "claim")
		c["state"] = "ready"
		c["case"] = map[string]any{

			"index": json.Number("0"),

			"scenario": "archive-note@1",

			"worldId": strings.Repeat("a", 32),

			"generation": json.Number("1"),

			"expiresAt": "2099-01-01T00:00:00.000Z",
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
