package sessionflow

import (
	"context"
	"github.com/vlno-ai/sdk/cli/internal/assessment"
	"reflect"
	"regexp"
)

var actorReference = regexp.MustCompile(`^sa_[a-f0-9]{32}$`)

func (p *Protocol) Admit(ctx context.Context) error {
	j, e := p.read()
	if e != nil {
		return e
	}
	a := object(j, "admission")
	if a["state"] == "confirmed" {
		return nil
	}
	if _, e = p.identity(ctx, j, a["actor"]); e != nil {
		return e
	}
	if e = p.Store.Update(func(j map[string]any) error { object(j, "admission")["state"] = "unknown"; return nil }); e != nil {
		return e
	}
	body := object(a, "command")
	source := object(j, "source")
	key := text(a["idempotencyKey"])
	value, e := p.Client.Request(ctx, "POST", "/v1/assessments/"+text(source["assessmentId"])+"/runs", body, key)
	if e != nil {
		return p.dispatchError(e, "admission_unknown")
	}
	if !assessment.Admission(value, text(source["assessmentId"]), body, key) || value["orgId"] != j["orgId"] ||
		!reflect.DeepEqual(value["actor"], a["actor"]) ||
		!reflect.DeepEqual(value["source"], source) {
		return p.dispatchError(code("admission_outcome_unknown"), "admission_unknown")
	}
	return p.Store.Update(func(j map[string]any) error {
		a := object(j, "admission")
		a["state"] = "confirmed"
		a["receipt"] = value
		j["recovery"] = nil
		return nil
	})
}

// Refresh is independently authorized; it never changes immutable intents.
func (p *Protocol) Refresh(ctx context.Context) error {
	j, e := p.read()
	if e != nil {
		return e
	}
	id := runID(j)
	if id == "" {
		return code("admission_outcome_unknown")
	}
	if _, e = p.identity(ctx, j, nil); e != nil {
		return e
	}
	value, e := p.Client.Request(ctx, "GET", "/v1/world-runs/"+id+"/assessment-run", nil, "")
	if e != nil {
		return e
	}
	if !assessment.Status(value, id) || value["orgId"] != j["orgId"] || !reflect.DeepEqual(value["source"], j["source"]) ||
		!reflect.DeepEqual(value["admittedBy"], object(j, "admission")["actor"]) {
		return code("invalid_assessment_response")
	}
	return p.Store.Update(func(j map[string]any) error {
		j["observation"] = map[string]any{"checkedAt": now(), "value": value}
		return nil
	})
}
