package sessionflow

import (
	"context"
	"github.com/vlno-ai/sdk/cli/internal/session"
)

func (p *Protocol) Cancel(ctx context.Context, allowNew bool) error {
	j, e := p.read()
	if e != nil {
		return e
	}
	if runID(j) == "" {
		return code("admission_outcome_unknown")
	}
	c := object(j, "cancellation")
	if c["state"] == "requested" {
		return p.Refresh(ctx)
	}
	if c["state"] == "not_requested" && !allowNew {
		return nil
	}
	actor, e := p.identity(ctx, j, c["actor"])
	if e != nil {
		return e
	}
	if c["state"] == "not_requested" {
		if e = p.Store.Update(func(j map[string]any) error {
			c := object(j, "cancellation")
			c["state"] = "unknown"
			c["actor"] = actor
			return nil
		}); e != nil {
			return e
		}
	}
	reply, e := p.Client.Request(ctx, "POST", "/v1/world-runs/"+runID(j)+"/cancel", map[string]any{}, "")
	if e != nil {
		return p.dispatchError(e, "cancel_unknown")
	}
	if !p.Bindings.ValidRun(reply, runID(j)) || (reply["cancellationRequested"] != true && !p.Bindings.Terminal(reply)) {
		return p.dispatchError(code("cancel_outcome_unknown"), "cancel_unknown")
	}
	if e = p.Store.Update(func(j map[string]any) error {
		object(j, "cancellation")["state"] = "requested"
		j["recovery"] = nil
		return nil
	}); e != nil {
		return e
	}
	return p.Refresh(ctx)
}

func (p *Protocol) Resume(ctx context.Context) error {
	j, e := p.read()
	if e != nil {
		return e
	}
	if object(j, "admission")["state"] == "prepared" {
		return nil
	}
	actor := object(j, "claim")["actor"]
	if object(j, "admission")["state"] != "confirmed" {
		actor = object(j, "admission")["actor"]
	} else if object(j, "cancellation")["state"] == "unknown" {
		actor = object(j, "cancellation")["actor"]
	}
	if _, e = p.identity(ctx, j, actor); e != nil {
		return e
	}
	if state := object(j, "execution")["state"]; state == "start_unknown" || state == "running" {
		if e = p.Store.Update(func(j map[string]any) error { object(j, "execution")["state"] = "exit_unknown"; return nil }); e != nil {
			return e
		}
		if e = p.Gap("exit_unknown"); e != nil {
			return e
		}
	} else if object(j, "capture")["state"] == "open" {
		if e = p.Gap("capture_incomplete"); e != nil {
			return e
		}
	}
	if object(j, "admission")["state"] == "unknown" {
		if e = p.Admit(ctx); e != nil {
			return e
		}
	}
	j, e = p.read()
	if e != nil {
		return e
	}
	if object(j, "cancellation")["state"] == "unknown" {
		return p.Cancel(ctx, false)
	}
	if object(j, "claim")["state"] == "unknown" {
		if _, e = p.Claim(ctx, false); e != nil {
			return e
		}
	}
	if e = p.Flush(ctx); e != nil {
		return e
	}
	if e = p.Finish(ctx, "", false); e != nil {
		return e
	}
	return p.Refresh(ctx)
}

func now() string { return session.Now() }
