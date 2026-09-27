package command

import (
	"context"
	"errors"
	"time"

	"github.com/vlno-ai/sdk/cli/internal/api"
)

// App manifests describe a worker-local immutable image. The client never
// builds, uploads, pulls, or executes an image.
func (r *runner) apps(args []string) error {
	if len(args) == 0 {
		return fail("usage")
	}
	if args[0] == "status" {
		if len(args) != 2 || !worldID.MatchString(args[1]) {
			return fail("invalid_job_id")
		}
		c, e := r.client()
		if e != nil {
			return e
		}
		value, e := c.Request(r.ctx, "GET", "/v1/app-import-jobs/"+args[1], nil, "")
		if e != nil {
			return scenarioError(e, args[1], "")
		}
		if e = appJobResponse(value, args[1]); e != nil {
			return scenarioError(e, args[1], "")
		}
		return r.emit(value)
	}
	action := args[0]
	if action != "import" && action != "validate" {
		return fail("usage")
	}
	fs := flags("apps " + action)
	file := fs.String("file", "", "")
	supplied, noWait := "", false
	if action == "import" {
		fs.StringVar(&supplied, "idempotency-key", "", "")
		fs.BoolVar(&noWait, "no-wait", false, "")
	}
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || *file == "" {
		return fail("usage")
	}
	manifest, e := r.readObject(*file)
	if e != nil {
		return e
	}
	key, path := "", "/v1/app-contracts/validate"
	if action == "import" {
		key, e = replayKey(supplied)
		if e != nil {
			return e
		}
		path = "/v1/app-import-jobs"
	}
	c, e := r.client()
	if e != nil {
		return e
	}
	if key != "" {
		r.progress("Idempotency key:", key)
	}
	value, e := c.Request(r.ctx, "POST", path, manifest, key)
	if e != nil {
		return scenarioError(e, "", key)
	}
	if action == "validate" {
		if value["valid"] != true {
			return fail("invalid_response")
		}
		return r.emit(value)
	}
	id, ok := value["id"].(string)
	if !ok || !worldID.MatchString(id) {
		return scenarioError(fail("invalid_response"), "", key)
	}
	if e = appJobResponse(value, id); e != nil {
		return scenarioError(e, id, key)
	}
	r.progress("App import job:", id)
	if !noWait {
		value, e = r.waitAppImport(c, id, value)
		if e != nil {
			return scenarioError(e, id, key)
		}
	}
	value["idempotency_key"] = key
	return r.emit(value)
}

func appJobResponse(value map[string]any, id string) error {
	if value["id"] != id {
		return fail("invalid_response")
	}
	switch value["state"] {
	case "queued", "importing", "validating", "ready", "failed":
		return nil
	default:
		return fail("invalid_response")
	}
}

func (r *runner) waitAppImport(c *api.Client, id string, value map[string]any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	last := ""
	for {
		if e := appJobResponse(value, id); e != nil {
			return nil, e
		}
		state := value["state"].(string)
		if state == "ready" {
			return value, nil
		}
		if state == "failed" {
			return nil, fail("app_import_failed")
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
			return nil, &failure{Code: "app_import_wait_timeout", Outcome: "unknown"}
		case <-time.After(500 * time.Millisecond):
		}
		var e error
		value, e = c.Request(ctx, "GET", "/v1/app-import-jobs/"+id, nil, "")
		if e != nil {
			if ctx.Err() != nil && r.ctx.Err() == nil {
				return nil, &failure{Code: "app_import_wait_timeout", Outcome: "unknown"}
			}
			return nil, e
		}
	}
}
