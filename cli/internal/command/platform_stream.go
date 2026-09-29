package command

import (
	"context"
	"encoding/json"
	"github.com/vlno-ai/sdk/cli/internal/api"
	"strconv"
)

func productStreamRun(event api.StreamEvent, id string) (map[string]any, error) {
	run, ok := event.Data["run"].(map[string]any)
	if event.Data["schema"] != "vlno.world-stream/1" || !ok || !validProductRun(run, id) {
		return nil, fail("invalid_stream")
	}
	return run, nil
}

func streamFailure(event api.StreamEvent) error {
	if event.Event == "stream_error" {
		status, ok := event.Data["status"].(json.Number)
		n, err := status.Int64()
		if !ok || err != nil || n < 400 || n > 599 {
			return fail("invalid_stream")
		}
		return &api.Error{Code: "stream_http_error", Status: int(n)}
	}
	return nil
}

func (r *runner) productWait(ctx context.Context, c *api.Client, id string) (map[string]any, error) {
	var latest map[string]any
	err := c.WatchWorld(ctx, id, 0, func(event api.StreamEvent) (bool, error) {
		if err := streamFailure(event); err != nil {
			return false, err
		}
		if event.Event != "run" {
			return false, nil
		}
		run, err := productStreamRun(event, id)
		if err != nil {
			return false, err
		}
		if latest == nil || latest["state"] != run["state"] {
			r.progress("Run:", run["state"].(string))
		}
		latest = run
		return terminalRun(run), nil
	})
	return latest, err
}

func (r *runner) productReady(ctx context.Context, c *api.Client, id string, assignment map[string]any) error {
	return c.WatchWorld(ctx, id, 0, func(event api.StreamEvent) (bool, error) {
		if err := streamFailure(event); err != nil {
			return false, err
		}
		if event.Event != "run" {
			return false, nil
		}
		run, err := productStreamRun(event, id)
		if err != nil {
			return false, err
		}
		if terminalRun(run) {
			return false, fail("world_not_ready")
		}
		readiness, ok := event.Data["readiness"].([]any)
		if !ok || len(readiness) > 16 {
			return false, fail("invalid_stream")
		}
		for _, raw := range readiness {
			ready, ok := raw.(map[string]any)
			if !ok {
				return false, fail("invalid_stream")
			}
			if ready["caseIndex"] != assignment["index"] {
				continue
			}
			if ready["worldId"] != assignment["worldId"] {
				return false, fail("invalid_stream")
			}
			switch ready["state"] {
			case "ready":
				return true, nil
			case "creating":
				return false, nil
			default:
				return false, fail("world_not_ready")
			}
		}
		return false, nil
	})
}

// Watch emits complete, validated frames as JSON lines with --json.
func (r *runner) productWatch(c *api.Client, id string, after int64) error {
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	var latest map[string]any
	cursor := after
	err := c.WatchWorld(ctx, id, after, func(event api.StreamEvent) (bool, error) {
		if err := streamFailure(event); err != nil {
			return false, err
		}
		switch event.Event {
		case "run":
			run, err := productStreamRun(event, id)
			if err != nil {
				return false, err
			}
			latest = run
		case "trajectory":
			n, err := strconv.ParseInt(event.ID, 10, 64)
			events, ok := event.Data["events"].([]any)
			if latest == nil || err != nil || !ok || len(events) > 100 || event.Data["runId"] != id || event.Data["schema"] != "vlno.world-trajectory/1" || event.Data["nextCursor"] != json.Number(event.ID) || n < cursor {
				return false, fail("invalid_stream")
			}
			last := cursor
			for _, raw := range events {
				item, ok := raw.(map[string]any)
				if !ok {
					return false, fail("invalid_stream")
				}
				seq, ok := item["sequence"].(json.Number)
				if !ok {
					return false, fail("invalid_stream")
				}
				value, err := seq.Int64()
				index, ok := item["caseIndex"].(json.Number)
				if !ok {
					return false, fail("invalid_stream")
				}
				ix, e := index.Int64()
				if err != nil || e != nil || value <= last || value > n || ix < 0 || ix > 15 {
					return false, fail("invalid_stream")
				}
				if item["source"] != "harness" && item["source"] != "environment" {
					return false, fail("invalid_stream")
				}
				last = value
			}
			if last != n {
				return false, fail("invalid_stream")
			}
			cursor = n
			if len(events) == 0 {
				return false, nil
			}
		case "done":
			if latest == nil || !terminalRun(latest) || event.Data["runId"] != id || event.Data["nextCursor"] != json.Number(strconv.FormatInt(cursor, 10)) {
				return false, fail("invalid_stream")
			}
			return true, r.emit(map[string]any{"event": event.Event, "data": event.Data})
		default:
			return false, fail("invalid_stream")
		}
		return false, r.emit(map[string]any{"event": event.Event, "data": event.Data})
	})
	if err != nil {
		return runError(err, id, "")
	}
	return nil
}
