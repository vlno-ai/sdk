package command

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/vlno-ai/sdk/cli/internal/api"
	"github.com/vlno-ai/sdk/cli/internal/config"
	"os"
)

func (r *runner) productNext(c *api.Client, settings config.Config, id, claimPath, outPath string) error {
	f, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail("connection_file_unavailable")
	}
	complete := false
	defer func() {
		f.Close()
		if !complete {
			os.Remove(outPath)
		}
	}()
	claim, err := productClaim(claimPath, true)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	path := "/v1/world-runs/" + id
	current, err := c.Request(ctx, "GET", path, nil, "")
	if err != nil {
		return runError(err, id, "")
	}
	if !validProductRun(current, id) {
		return runError(fail("invalid_response"), id, "")
	}
	if terminalRun(current) {
		return r.emit(map[string]any{"run": current, "case": nil})
	}
	for {
		value, err := c.Request(ctx, "POST", path+"/next", map[string]any{"claim": claim}, "")
		if err != nil {
			var remote *api.Error
			if errors.As(err, &remote) && remote.Status == 503 && remote.Code == "run_preparing" && remote.Outcome != "unknown" {
				if err = productPause(ctx); err != nil {
					return runError(err, id, "")
				}
				continue
			}
			return runError(err, id, "")
		}
		run, ok := value["run"].(map[string]any)
		if !ok || !validProductRun(run, id) {
			return runError(fail("invalid_response"), id, "")
		}
		raw, exists := value["case"]
		if exists && raw == nil && terminalRun(run) {
			return r.emit(map[string]any{"run": run, "case": nil})
		}
		assignment, ok := raw.(map[string]any)
		if !ok {
			return runError(fail("invalid_response"), id, "")
		}
		manifest, err := productConnection(assignment, run, settings)
		if err != nil {
			return runError(err, id, "")
		}
		if assignment["state"] == "creating" {
			if err = r.productReady(ctx, c, id, assignment); err != nil {
				return runError(err, id, "")
			}
			assignment["state"] = "ready"
		}
		if assignment["state"] == "ready" {
			manifest, err = productConnection(assignment, run, settings)
			if err != nil {
				return runError(err, id, "")
			}
			if json.NewEncoder(f).Encode(manifest) != nil || f.Sync() != nil || f.Close() != nil {
				return runError(fail("connection_file_unavailable"), id, "")
			}
			complete = true
			return r.emit(map[string]any{"run_id": id, "case_index": assignment["index"], "scenario": assignment["scenario"], "world_id": assignment["worldId"], "connection_file": outPath, "expires_at": assignment["expiresAt"]})
		}
		if assignment["state"] != "creating" {
			return runError(fail("world_not_ready"), id, "")
		}
		if err = productPause(ctx); err != nil {
			return runError(err, id, "")
		}
	}
}
