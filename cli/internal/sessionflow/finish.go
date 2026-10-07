package sessionflow

import (
	"context"
	"encoding/json"
)

// Finish never infers success from a process exit or from a resumed session.
func (p *Protocol) prepareFinish(ctx context.Context, status string, allowNew bool) error {
	j, e := p.read()
	if e != nil {
		return e
	}
	f := object(j, "finish")
	c := object(j, "claim")
	if f["state"] != "not_started" && status != "" && object(f, "command")["agent_status"] != status {
		return code("session_finish_conflict")
	}
	if f["state"] == "accepted" {
		return nil
	}
	if f["state"] == "not_started" && !allowNew {
		return nil
	}
	if c["case"] == nil {
		return code("session_case_unavailable")
	}
	actor, e := p.identity(ctx, j, c["actor"])
	if e != nil {
		return e
	}
	if f["state"] == "not_started" {
		if status != "completed" && status != "error" && status != "timeout" {
			return code("session_finish_invalid")
		}
		external := object(j, "execution")["mode"] == "external"
		if status == "completed" && external && object(j, "capture")["state"] != "incomplete" {
			if e = p.Gap("capture_incomplete"); e != nil {
				return e
			}
		}
		if status == "completed" && !external && (object(j, "capture")["state"] != "complete" || object(j, "execution")["state"] != "exited") {
			return code("session_capture_incomplete")
		}
		if e = p.Store.Update(func(j map[string]any) error {
			f := object(j, "finish")
			f["state"] = "unknown"
			f["actor"] = actor
			f["command"] = map[string]any{"case_index": json.Number("0"), "agent_status": status}
			return nil
		}); e != nil {
			return e
		}
	}
	return nil
}

// PrepareFinish records an exact declaration without transmitting any mutation.
func (p *Protocol) PrepareFinish(ctx context.Context, status string) error {
	return p.prepareFinish(ctx, status, true)
}
func (p *Protocol) Finish(ctx context.Context, status string, allowNew bool) error {
	if e := p.prepareFinish(ctx, status, allowNew); e != nil {
		return e
	}
	j, e := p.read()
	if e != nil {
		return e
	}
	f := object(j, "finish")
	if f["state"] == "not_started" || f["state"] == "accepted" {
		return nil
	}

	if e = p.Flush(ctx); e != nil {
		return e
	}
	j, e = p.read()
	if e != nil {
		return e
	}
	f = object(j, "finish")
	if _, e = p.identity(ctx, j, f["actor"]); e != nil {
		return e
	}
	secret, e := p.Store.PrivateClaim()
	if e != nil {
		return e
	}
	command := object(f, "command")
	reply, e := p.Client.Request(ctx, "POST", "/v1/world-runs/"+runID(j)+"/finish", map[string]any{

		"claim": secret,

		"case_index": command["case_index"],

		"agent_status": command["agent_status"],
	}, "")
	if e != nil {
		return p.dispatchError(e, "finish_unknown")
	}
	if !p.Bindings.ValidRun(reply, runID(j)) {
		return p.dispatchError(code("finish_outcome_unknown"), "finish_unknown")
	}
	return p.Store.Update(func(j map[string]any) error {
		object(j, "finish")["state"] = "accepted"
		j["recovery"] = nil
		return nil
	})
}
