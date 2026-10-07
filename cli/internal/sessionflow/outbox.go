package sessionflow

import (
	"context"
	"encoding/json"
	"github.com/vlno-ai/sdk/cli/internal/session"
	"strconv"
)

func (p *Protocol) Record(kind string, data map[string]any, secrets []string, reserved bool) error {
	clean, e := Scrub(data, secrets)
	if e != nil {
		return e
	}
	id, e := session.NewID()
	if e != nil {
		return e
	}
	event := map[string]any{

		"id": id,

		"kind": kind,

		"occurredAt": now(),

		"data": clean,
	}
	bytes, e := session.Encode(event)
	if e != nil {
		return e
	}
	limit := 24000
	if reserved {
		limit = 1024
	}
	if len(bytes) > limit || session.PythonTransportSize(event) > 65536 {
		return code("session_event_too_large")
	}
	return p.Store.Update(func(j map[string]any) error {
		c := object(j, "capture")
		if c["state"] == "complete" || c["state"] == "incomplete" || c["gapRecorded"] == true {
			return code("session_capture_closed")
		}
		rows := c["events"].([]any)
		total := len(bytes)
		for _, row := range rows {
			b, e := session.Encode(object(row.(map[string]any), "event"))
			if e != nil {
				return e
			}
			total += len(b)
		}
		count, maximum := 1000, 768*1024
		if reserved {
			count = 1024
			maximum = 1024 * 1024
		}
		if len(rows) >= count || total > maximum {
			return code("outbox_full")
		}
		c["events"] = append(rows, map[string]any{"sequence": c["nextSequence"], "event": event})
		c["nextSequence"] = json.Number(strconv.Itoa(len(rows) + 2))
		if c["state"] == "not_started" {
			c["state"] = "open"
		}
		return nil
	})
}
func (p *Protocol) Gap(reason string) error {
	j, e := p.read()
	if e != nil {
		return e
	}
	if object(j, "capture")["gapRecorded"] == true {
		return nil
	}
	id, e := session.NewID()
	if e != nil {
		return e
	}
	return p.Store.Update(func(j map[string]any) error {
		c := object(j, "capture")
		rows := c["events"].([]any)
		c["events"] = append(rows, map[string]any{
			"sequence": c["nextSequence"],
			"event": map[string]any{

				"id": id,

				"kind": "gap",

				"occurredAt": now(),

				"data": map[string]any{"event": "capture_gap", "reason": reason},
			},
		})
		c["nextSequence"] = json.Number(strconv.Itoa(len(rows) + 2))
		c["gapRecorded"] = true
		c["state"] = "incomplete"
		j["recovery"] = map[string]any{"code": reason, "occurredAt": now()}
		return nil
	})
}
func (p *Protocol) Flush(ctx context.Context) error {
	for {
		j, e := p.read()
		if e != nil {
			return e
		}
		c := object(j, "capture")
		rows := c["events"].([]any)
		ack := integer(c["acknowledgedThrough"])
		if ack == int64(len(rows)) {
			return nil
		}
		if _, e = p.identity(ctx, j, object(j, "claim")["actor"]); e != nil {
			return e
		}
		secret, e := p.Store.PrivateClaim()
		if e != nil {
			return e
		}
		event := object(rows[ack].(map[string]any), "event")
		reply, e := p.Client.Request(ctx, "POST", "/v1/world-runs/"+runID(j)+"/cases/0/transcript", map[string]any{"claim": secret, "events": []any{event}}, "")
		if e != nil {
			return e
		}
		ids, ok := reply["accepted"].([]any)
		if !ok || len(ids) != 1 || ids[0] != event["id"] {
			return code("invalid_transcript_acknowledgement")
		}
		if e = p.Store.Update(func(j map[string]any) error {
			capture := object(j, "capture")
			if integer(capture["acknowledgedThrough"]) != ack {
				return session.ErrCorrupt
			}
			capture["acknowledgedThrough"] = json.Number(strconv.FormatInt(ack+1, 10))
			return nil
		}); e != nil {
			return e
		}
	}
}
func (p *Protocol) Complete() error {
	return p.Store.Update(func(j map[string]any) error { object(j, "capture")["state"] = "complete"; return nil })
}
