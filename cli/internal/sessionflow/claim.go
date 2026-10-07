package sessionflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/vlno-ai/sdk/cli/internal/api"
	"math/big"
	"reflect"
	"time"
)

// Claim returns an in-memory scoped handoff. It is never serialized in a journal.
func (p *Protocol) Claim(ctx context.Context, allowNew bool) (map[string]any, error) {
	j, e := p.read()
	if e != nil {
		return nil, e
	}
	c := object(j, "claim")
	id := runID(j)
	if id == "" {
		return nil, code("admission_outcome_unknown")
	}
	if c["state"] == "ended" || c["state"] == "not_started" && !allowNew {
		return nil, nil
	}
	actor, e := p.identity(ctx, j, c["actor"])
	if e != nil {
		return nil, e
	}
	if c["state"] == "not_started" {
		if e = p.Store.Update(func(j map[string]any) error {
			c := object(j, "claim")
			c["state"] = "unknown"
			c["actor"] = actor
			return nil
		}); e != nil {
			return nil, e
		}
	}
	secret, e := p.Store.PrivateClaim()
	if e != nil {
		return nil, e
	}
	for {
		j, e = p.read()
		if e != nil {
			return nil, e
		}
		if _, e = p.identity(ctx, j, actor); e != nil {
			return nil, e
		}
		reply, e := p.Client.Request(ctx, "POST", "/v1/world-runs/"+id+"/next", map[string]any{"claim": secret}, "")
		if e != nil {
			var remote *api.Error
			if errors.As(e, &remote) && remote.Status == 503 && remote.Code == "run_preparing" && remote.Outcome != "unknown" {
				if e = pause(ctx); e == nil {
					continue
				}
			}
			return nil, p.dispatchError(e, "assignment_unknown")
		}
		run := object(reply, "run")
		if !p.Bindings.ValidRun(run, id) {
			return nil, p.dispatchError(code("invalid_response"), "assignment_unknown")
		}
		raw, exists := reply["case"]
		if exists && raw == nil && p.Bindings.Terminal(run) {
			e = p.Store.Update(func(j map[string]any) error { object(j, "claim")["state"] = "ended"; return nil })
			return nil, e
		}
		assigned, ok := raw.(map[string]any)
		if !ok || assigned["state"] != "ready" || assigned["scenario"] != "archive-note@1" || integer(assigned["index"]) != 0 ||
			assigned["index"] != json.Number("0") {
			return nil, p.dispatchError(code("invalid_response"), "assignment_unknown")
		}
		rawConnection := object(assigned, "connection")
		if rawConnection["environment"] != nil {
			return nil, code("invalid_connection_manifest")
		}
		handoff, e := p.Bindings.Connection(assigned, run)
		if e != nil {
			return nil, p.dispatchError(e, "assignment_unknown")
		}
		expiry, e := millisecondExpiry(assigned["expiresAt"])
		if e != nil {
			return nil, e
		}
		snapshot := map[string]any{

			"index": json.Number("0"),

			"scenario": "archive-note@1",

			"worldId": assigned["worldId"],

			"generation": json.Number("1"),

			"expiresAt": expiry,
		}
		if previous := object(j, "claim")["case"]; previous != nil && !reflect.DeepEqual(previous, snapshot) {
			return nil, code("session_assignment_changed")
		}
		e = p.Store.Update(func(j map[string]any) error {
			c := object(j, "claim")
			c["case"] = snapshot
			c["state"] = "ready"
			j["recovery"] = nil
			return nil
		})
		return handoff, e
	}
}
func millisecondExpiry(v any) (string, error) {
	n, ok := v.(json.Number)
	if !ok {
		return "", code("invalid_connection_manifest")
	}
	r, ok := new(big.Rat).SetString(string(n))
	if !ok || r.Sign() <= 0 {
		return "", code("invalid_connection_manifest")
	}
	scaled := new(big.Int).Mul(r.Num(), big.NewInt(1000))
	ms := new(big.Int).Quo(scaled, r.Denom())
	if !ms.IsInt64() {
		return "", code("invalid_connection_manifest")
	}
	t := time.UnixMilli(ms.Int64()).UTC()
	if t.Year() < 1 || t.Year() > 9999 || !t.After(time.Now()) {
		return "", code("connection_expired")
	}
	return t.Format("2006-01-02T15:04:05.000Z"), nil
}
func pause(ctx context.Context) error {
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
