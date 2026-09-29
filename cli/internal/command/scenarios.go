package command

import (
	"context"
	"errors"
	"time"

	"github.com/vlno-ai/sdk/cli/internal/api"
)

func (r *runner) scenarios(args []string) error {
	if len(args) == 0 {
		return fail("usage")
	}
	if args[0] == "generate" {
		return r.generateScenarios(args[1:])
	}
	if args[0] != "status" && args[0] != "freeze" {
		return fail("usage")
	}
	if len(args) < 2 || !worldID.MatchString(args[1]) {
		return fail("invalid_job_id")
	}
	id := args[1]
	path, method := "/v1/scenario-jobs/"+id, "GET"
	var payload any
	if args[0] == "status" {
		if len(args) != 2 {
			return fail("usage")
		}
	} else {
		fs := flags("freeze")
		suite := fs.String("suite", "", "")
		if fs.Parse(args[2:]) != nil || fs.NArg() != 0 {
			return fail("usage")
		}
		if !versionRef.MatchString(*suite) {
			return fail("invalid_suite_ref")
		}
		path, method, payload = path+"/freeze", "POST", map[string]any{"suite": *suite}
	}
	c, e := r.client()
	if e != nil {
		return e
	}
	value, e := c.Request(r.ctx, method, path, payload, "")
	if e != nil {
		return scenarioError(e, id, "")
	}
	return r.emit(value)
}

func (r *runner) generateScenarios(args []string) error {
	fs := flags("generate")
	family := fs.String("family", "", "")
	template := fs.String("template", "", "")
	seed := fs.Int64("seed", -1, "")
	count := fs.Int("count", 2, "")
	distractors := fs.Int("distractors", 2, "")
	supplied := fs.String("idempotency-key", "", "")
	noWait := fs.Bool("no-wait", false, "")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return fail("usage")
	}
	if *family != "archive-note" && *family != "archive-note-integrity" && *family != "close-ticket" {
		return fail("invalid_family")
	}
	if !versionRef.MatchString(*template) {
		return fail("invalid_version_ref")
	}
	if *seed < 0 || *seed > 2147483647 {
		return fail("invalid_seed")
	}
	if *count < 1 || *count > 4 || *distractors < 1 || *distractors > 4 {
		return fail("invalid_generation_count")
	}
	key, e := replayKey(*supplied)
	if e != nil {
		return e
	}
	c, e := r.client()
	if e != nil {
		return e
	}
	payload := map[string]any{"family": *family, "template": *template, "seed": *seed, "count": *count, "distractors": *distractors}
	r.progress("Idempotency key:", key)
	value, e := c.Request(r.ctx, "POST", "/v1/scenario-jobs", payload, key)
	if e != nil {
		return scenarioError(e, "", key)
	}
	id, ok := value["id"].(string)
	if !ok || !worldID.MatchString(id) {
		return scenarioError(fail("invalid_response"), "", key)
	}
	r.progress("Scenario job:", id)
	if !*noWait {
		value, e = r.waitScenario(c, id, value)
		if e != nil {
			return scenarioError(e, id, key)
		}
	}
	value["idempotency_key"] = key
	return r.emit(value)
}

func scenarioError(err error, id, key string) error {
	f := lifecycleError(err, "", key).(*failure)
	f.JobID = id
	return f
}

func (r *runner) waitScenario(c *api.Client, id string, value map[string]any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	last := ""
	for {
		if value["id"] != id {
			return nil, fail("invalid_response")
		}
		state, _ := value["state"].(string)
		switch state {
		case "validated":
			return value, nil
		case "failed":
			return nil, fail("scenario_job_failed")
		case "queued", "running":
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
			return nil, &failure{Code: "scenario_wait_timeout", Outcome: "unknown"}
		case <-time.After(500 * time.Millisecond):
		}
		var e error
		value, e = c.Request(ctx, "GET", "/v1/scenario-jobs/"+id, nil, "")
		if e != nil {
			if ctx.Err() != nil && r.ctx.Err() == nil {
				return nil, &failure{Code: "scenario_wait_timeout", Outcome: "unknown"}
			}
			return nil, e
		}
	}
}
