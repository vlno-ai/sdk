package command

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/vlno-ai/sdk/cli/internal/api"
)

var revisionLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func runError(err error, id, key string) error {
	f := lifecycleError(err, "", key).(*failure)
	f.RunID = id
	if errors.Is(err, context.DeadlineExceeded) {
		f.Code = "suite_wait_timeout"
		f.Outcome = "unknown"
	}
	return f
}
func terminalRun(value map[string]any) bool {
	return value["state"] == "completed" || value["state"] == "cancelled" || value["state"] == "failed"
}
func validRun(value map[string]any, id string) bool {
	if value["id"] != id {
		return false
	}
	switch value["state"] {
	case "pending", "running", "completed", "cancelling", "cancelled", "failed":
		return true
	}
	return false
}
func passedRun(value map[string]any) bool {
	if value["state"] != "completed" {
		return false
	}
	cases, ok := value["cases"].([]any)
	if !ok || len(cases) == 0 {
		return false
	}
	for _, raw := range cases {
		c, ok := raw.(map[string]any)
		if !ok || c["state"] != "passed" || c["cleanup_confirmed"] != true {
			return false
		}
	}
	return true
}
func (r *runner) runs(args []string) error {
	if len(args) < 2 {
		return fail("usage")
	}
	action := args[0]
	if action != "status" && action != "cancel" && action != "compare" {
		return fail("usage")
	}
	if !worldID.MatchString(args[1]) {
		return fail("invalid_run_id")
	}
	path, method := "/v1/suite-runs/"+args[1], "GET"
	var payload any
	if action == "compare" {
		if len(args) != 3 || !worldID.MatchString(args[2]) {
			return fail("invalid_run_id")
		}
		path, method, payload = "/v1/suite-run-comparisons", "POST", map[string]any{"baseline": args[1], "candidate": args[2]}
	} else if len(args) != 2 {
		return fail("usage")
	} else if action == "cancel" {
		path += "/cancel"
		method = "POST"
		payload = map[string]any{}
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	value, err := c.Request(r.ctx, method, path, payload, "")
	if err != nil {
		return runError(err, args[1], "")
	}
	if action != "compare" && !validRun(value, args[1]) {
		return runError(fail("invalid_response"), args[1], "")
	}
	if err = r.emit(value); err != nil {
		return err
	}
	if action == "compare" {
		counts, ok := value["counts"].(map[string]any)
		regressions, rok := counts["regression"].(json.Number)
		incomparable, iok := counts["incomparable"].(json.Number)
		if !ok || !rok || !iok || regressions.String() != "0" || incomparable.String() != "0" {
			return evaluationUnpassed
		}
	}
	return nil
}
func (r *runner) suites(args []string) (resultErr error) {
	if len(args) < 2 || args[0] != "run" {
		return fail("usage")
	}
	ref := args[1]
	if !versionRef.MatchString(ref) {
		return fail("invalid_suite_ref")
	}
	fs := flags("suites run")
	environment := fs.Bool("environment", false, "")
	revision := fs.String("revision", "", "")
	file := fs.String("agent-config", "", "")
	harness := fs.String("harness", "", "")
	supplied := fs.String("idempotency-key", "", "")
	deadline := fs.Duration("case-timeout", 300*time.Second, "")
	if fs.Parse(args[2:]) != nil || fs.NArg() != 0 || ((*file == "") == (*harness == "")) {
		return fail("usage")
	}
	if !revisionLabel.MatchString(*revision) {
		return fail("invalid_revision")
	}
	if *deadline <= 0 || *deadline > 1500*time.Second {
		return fail("invalid_case_timeout")
	}
	var cfg agentConfig
	var err error
	if *harness != "" {
		if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`).MatchString(*harness) {
			return fail("invalid_harness")
		}
		*environment = true
	} else {
		cfg, err = loadAgentConfig(*file)
		if err != nil {
			return err
		}
	}
	key, err := replayKey(*supplied)
	if err != nil {
		return err
	}
	claim, err := replayKey("")
	if err != nil {
		return err
	}
	settings, err := r.settings()
	if err != nil {
		return err
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	payload := map[string]any{"suite": ref, "revision": *revision, "ttl_seconds": 1800}
	if *environment {
		payload["environment"] = true
	}
	if *harness != "" {
		payload["harness"] = *harness
	}
	value, err := c.Request(ctx, "POST", "/v1/suite-runs", payload, key)
	if err != nil {
		return runError(err, "", key)
	}
	id, ok := value["id"].(string)
	if !ok || !worldID.MatchString(id) || !validRun(value, id) {
		return runError(fail("invalid_response"), "", key)
	}
	r.progress("Run:", id)
	r.progress("Idempotency key:", key)
	owned := false
	defer func() {
		if resultErr != nil && !errors.Is(resultErr, evaluationUnpassed) && owned {
			cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			_, _ = c.Request(cleanupCtx, "POST", "/v1/suite-runs/"+id+"/cancel", map[string]any{}, "")
		}
	}()
	for !terminalRun(value) {
		next, e := c.Request(ctx, "POST", "/v1/suite-runs/"+id+"/next", map[string]any{"claim": claim}, "")
		if e != nil {
			return runError(e, id, key)
		}
		value, ok = next["run"].(map[string]any)
		if !ok || !validRun(value, id) {
			return runError(fail("invalid_response"), id, key)
		}
		if next["case"] == nil {
			if !terminalRun(value) {
				return runError(fail("invalid_response"), id, key)
			}
			break
		}
		owned = true
		current, ok := next["case"].(map[string]any)
		if !ok {
			return runError(fail("invalid_response"), id, key)
		}
		index, ok := current["index"].(json.Number)
		if !ok {
			return runError(fail("invalid_response"), id, key)
		}
		n, e := index.Int64()
		if e != nil || n < 0 || n > 1024 {
			return runError(fail("invalid_response"), id, key)
		}
		caseRef, ok := current["ref"].(string)
		if !ok || !versionRef.MatchString(caseRef) {
			return runError(fail("invalid_response"), id, key)
		}
		world, ok := current["world"].(map[string]any)
		if !ok {
			return runError(fail("invalid_response"), id, key)
		}
		wid, ok := world["id"].(string)
		if !ok || !worldID.MatchString(wid) {
			return runError(fail("invalid_response"), id, key)
		}
		owned = true
		// Owner-only lifecycle polling is contained in the driver. The child receives
		// only the world capability and the public task/operation contract.
		for world["state"] != "ready" {
			if world["state"] != "creating" && world["state"] != "resetting" {
				return runError(fail("suite_world_failed"), id, key)
			}
			if e = pollDelay(ctx); e != nil {
				return runError(e, id, key)
			}
			world, e = c.Request(ctx, "GET", "/v1/worlds/"+wid, nil, "")
			if e != nil {
				return runError(e, id, key)
			}
			if world["id"] != wid {
				return runError(fail("invalid_response"), id, key)
			}
		}
		token, ok := world["agent_token"].(string)
		if !ok || !roleToken.MatchString(token) {
			return runError(fail("invalid_response"), id, key)
		}
		appClient, e := api.New(settings.Endpoint, token, settings.CAFile, r.opts.timeout)
		if e != nil {
			return runError(e, id, key)
		}
		task, e := appClient.Request(ctx, "GET", "/v1/worlds/"+wid+"/task", nil, "")
		if e != nil {
			return runError(e, id, key)
		}
		taskText, ok := task["task"].(string)
		if !ok {
			return runError(fail("invalid_response"), id, key)
		}
		operations, e := appClient.Request(ctx, "GET", "/v1/worlds/"+wid+"/operations", nil, "")
		if e != nil {
			return runError(e, id, key)
		}
		input := map[string]any{"task": taskText, "case_ref": caseRef, "app": map[string]any{"endpoint": settings.Endpoint, "world_id": wid, "token": token, "ca_file": settings.CAFile}, "operations": operations}
		if cfg.InputFormat == "connection-v1" {
			input, e = c.Request(ctx, "GET", "/v1/worlds/"+wid+"/connection", nil, "")
			if e != nil {
				return runError(e, id, key)
			}
			if e = resolveConnection(input, wid, settings.Endpoint, settings.CAFile, settings.APIKey); e != nil {
				return runError(e, id, key)
			}
		}
		remaining := *deadline
		if expires, ok := world["expires_at"].(json.Number); ok {
			seconds, parseErr := expires.Float64()
			if parseErr != nil {
				return runError(fail("invalid_response"), id, key)
			}
			lease := time.Until(time.UnixMilli(int64(seconds*1000))) - 15*time.Second
			if lease <= 0 {
				return runError(fail("world_lease_expiring"), id, key)
			}
			if lease < remaining {
				remaining = lease
			}
		}
		var status string
		if *harness != "" {
			status, e = executeWorldHarness(ctx, remaining, c, wid, claim)
		} else {
			status, e = executeSuiteAgent(ctx, remaining, cfg, input, agentEnv(cfg, settings.APIKey, claim, key), c, id)
		}
		if e != nil {
			return runError(e, id, key)
		}
		if ctx.Err() != nil {
			return runError(ctx.Err(), id, key)
		}
		value, e = c.Request(ctx, "POST", "/v1/suite-runs/"+id+"/finish", map[string]any{"case_index": n, "claim": claim, "agent_status": status}, "")
		if e != nil {
			return runError(e, id, key)
		}
		for {
			if !validRun(value, id) {
				return runError(fail("invalid_response"), id, key)
			}
			if terminalRun(value) {
				break
			}
			cases, ok := value["cases"].([]any)
			if !ok || int(n) >= len(cases) {
				return runError(fail("invalid_response"), id, key)
			}
			record, ok := cases[n].(map[string]any)
			if !ok {
				return runError(fail("invalid_response"), id, key)
			}
			if record["state"] != "running" && record["state"] != "finishing" {
				break
			}
			if e = pollDelay(ctx); e != nil {
				return runError(e, id, key)
			}
			value, e = c.Request(ctx, "GET", "/v1/suite-runs/"+id, nil, "")
			if e != nil {
				return runError(e, id, key)
			}
		}
	}
	value["idempotency_key"] = key
	if err = r.emit(value); err != nil {
		return err
	}
	if !passedRun(value) {
		return evaluationUnpassed
	}
	return nil
}
func pollDelay(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(250 * time.Millisecond):
		return nil
	}
}

// Monitor owner-side cancellation while the agent runs; ownership and hidden
// evaluation data stay in this process and never reach the child.
func executeSuiteAgent(ctx context.Context, deadline time.Duration, cfg agentConfig, input map[string]any, env []string, c *api.Client, id string) (string, error) {
	childCtx, stop := context.WithTimeout(ctx, deadline)
	defer stop()
	result := make(chan string, 1)
	go func() { result <- executeAgent(childCtx, cfg, input, env) }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case status := <-result:
			return status, nil
		case <-ticker.C:
			requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			value, err := c.Request(requestCtx, "GET", "/v1/suite-runs/"+id, nil, "")
			cancel()
			if err == nil && !validRun(value, id) {
				err = fail("invalid_response")
			}
			if err == nil && (terminalRun(value) || value["state"] == "cancelling") {
				err = fail("suite_run_stopped")
			}
			if err != nil {
				stop()
				<-result
				return "", err
			}
		}
	}
}

// The worker launches one pinned customer command inside the world's workstation.
func executeWorldHarness(ctx context.Context, deadline time.Duration, c *api.Client, world, key string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, deadline+30*time.Second)
	defer cancel()
	seconds := int(deadline.Seconds())
	if seconds > 1200 {
		seconds = 1200
	}
	if seconds < 1 {
		return "timeout", nil
	}
	path := "/v1/worlds/" + world + "/harness"
	job, err := c.Request(ctx, "POST", path, map[string]any{"timeout_seconds": seconds}, key)
	if err != nil {
		return "error", err
	}
	for job["state"] == "starting" || job["state"] == "running" {
		if err = pollDelay(ctx); err != nil {
			return "error", err
		}
		job, err = c.Request(ctx, "GET", path, nil, "")
		if err != nil {
			return "error", err
		}
	}
	if job["state"] == "timeout" {
		return "timeout", nil
	}
	if job["state"] == "completed" && job["error"] == nil {
		return "completed", nil
	}
	return "error", nil
}
