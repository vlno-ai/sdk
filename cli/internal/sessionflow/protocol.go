// Package sessionflow recovers exact durable protocol intents. It never starts a process.
package sessionflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/vlno-ai/sdk/cli/internal/session"
	"reflect"
)

type Client interface {
	Request(context.Context, string, string, any, string) (map[string]any, error)
}
type Bindings struct {
	ValidRun   func(map[string]any, string) bool
	Terminal   func(map[string]any) bool
	Connection func(map[string]any, map[string]any) (map[string]any, error)
}
type Protocol struct {
	Store    *session.Store
	Client   Client
	Endpoint string
	Bindings Bindings
}

func object(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }
func text(v any) string                                { s, _ := v.(string); return s }
func code(s string) error                              { return errors.New(s) }
func integer(v any) int64                              { n, _ := v.(json.Number); i, _ := n.Int64(); return i }
func runID(j map[string]any) string                    { return text(object(object(j, "admission"), "receipt")["runId"]) }
func (p *Protocol) read() (map[string]any, error) {
	j, e := p.Store.Read()
	if e != nil {
		return nil, e
	}
	origin, e := session.Origin(p.Endpoint)
	if e != nil || origin != j["endpoint"] {
		return nil, code("session_identity_changed")
	}
	return j, nil
}
func (p *Protocol) identity(ctx context.Context, j map[string]any, expected any) (map[string]any, error) {
	who, e := p.Client.Request(ctx, "GET", "/v1/assessment-runs/access", nil, "")
	if e != nil {
		return nil, e
	}
	a := object(who, "actor")
	ref := text(a["reference"])
	if len(who) != 3 || who["schema"] != "vlno.assessment-run-access/1" || who["orgId"] != j["orgId"] || len(a) != 2 ||
		(a["kind"] != "human" && a["kind"] != "service_key") ||
		!actorReference.MatchString(ref) {
		return nil, code("session_identity_changed")
	}
	if expected != nil && !reflect.DeepEqual(a, expected) {
		return nil, code("session_identity_changed")
	}
	return a, nil
}
func (p *Protocol) recover(codeValue string) error {
	return p.Store.Update(func(j map[string]any) error {
		j["recovery"] = map[string]any{"code": codeValue, "occurredAt": session.Now()}
		return nil
	})
}
func (p *Protocol) dispatchError(err error, recovery string) error {
	if e := p.recover(recovery); e != nil {
		return e
	}
	return err
}

func stringsOf(v any) []string {
	items, _ := v.([]any)
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = text(item)
	}
	return result
}
