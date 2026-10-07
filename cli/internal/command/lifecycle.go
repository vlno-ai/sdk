package command

import (
	"context"
	"errors"
	"github.com/vlno-ai/sdk/cli/internal/api"
	"time"
)

func lifecycleError(e error, id, key string) error {
	f := &failure{Code: "command_failed", WorldID: id, Key: key}
	var a *api.Error
	var own *failure
	if errors.As(e, &a) {
		f.Code = a.Code
		f.Status = a.Status
		f.Outcome = a.Outcome
		f.Reason = a.Reason
		f.Stage = a.Stage
	}
	if errors.As(e, &own) {
		f.Code = own.Code
		f.Outcome = own.Outcome
	}
	if errors.Is(e, context.Canceled) {
		f.Code = "cancelled"
		f.Outcome = "unknown"
	}
	return f
}

func (r *runner) wait(c *api.Client, id, target string, value map[string]any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	last := ""
	for {
		state, ok := value["state"].(string)
		if !ok {
			return nil, fail("invalid_response")
		}
		if state == target {
			return value, nil
		}
		switch state {
		case "failed", "closed", "cleanup_failed":
			return nil, fail("world_" + state)
		case "creating", "ready", "resetting", "deleting":
		default:
			return nil, fail("invalid_response")
		}
		if state != last {
			r.progress("State:", state)
			last = state
		}
		select {
		case <-ctx.Done():
			if errors.Is(r.ctx.Err(), context.Canceled) {
				return nil, context.Canceled
			}
			return nil, &failure{Code: "world_wait_timeout", Outcome: "unknown"}
		case <-time.After(500 * time.Millisecond):
		}
		var e error
		value, e = c.Request(ctx, "GET", "/v1/worlds/"+id, nil, "")
		if e != nil {
			if ctx.Err() != nil && r.ctx.Err() == nil {
				return nil, &failure{Code: "world_wait_timeout", Outcome: "unknown"}
			}
			return nil, e
		}
	}
}
