package command

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/vlno-ai/sdk/cli/internal/api"
)

var basisHash = regexp.MustCompile(`^[a-f0-9]{64}$`)
var roleToken = regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)

func (r *runner) authoring(args []string) error {
	if len(args) == 0 {
		return fail("usage")
	}
	if args[0] == "create" {
		return r.createAuthoring(args[1:])
	}
	action := args[0]
	switch action {
	case "status", "context", "validate", "run", "live", "freeze", "role", "submit":
	default:
		return fail("usage")
	}
	if len(args) < 2 || !worldID.MatchString(args[1]) {
		return fail("invalid_job_id")
	}
	id, tail := args[1], args[2:]
	if action == "role" {
		return r.assignAuthoring(id, tail)
	}
	path, method := "/v1/authoring-jobs/"+id, "GET"
	var payload any
	fs := flags(action)
	noWait := false
	switch action {
	case "run", "live", "validate":
		fs.BoolVar(&noWait, "no-wait", false, "")
		if fs.Parse(tail) != nil || fs.NArg() != 0 {
			return fail("usage")
		}
		path, method, payload = path+"/"+action, "POST", map[string]any{}
	case "freeze":
		suite := fs.String("suite", "", "")
		if fs.Parse(tail) != nil || fs.NArg() != 0 {
			return fail("usage")
		}
		if !versionRef.MatchString(*suite) {
			return fail("invalid_suite_ref")
		}
		path, method, payload = path+"/freeze", "POST", map[string]any{"suite": *suite}
	case "submit":
		file := fs.String("file", "", "")
		basis := fs.String("basis", "", "")
		if fs.Parse(tail) != nil || fs.NArg() != 0 || *file == "" {
			return fail("usage")
		}
		if !basisHash.MatchString(*basis) {
			return fail("invalid_basis_hash")
		}
		artifact, e := r.readObject(*file)
		if e != nil {
			return e
		}
		path, method, payload = path+"/artifacts", "POST", map[string]any{"basis_sha256": *basis, "artifact": artifact}
	default:
		if len(tail) != 0 {
			return fail("usage")
		}
		if action == "context" {
			path += "/context"
		}
	}
	c, e := r.client()
	if e != nil {
		return e
	}
	value, e := c.Request(r.ctx, method, path, payload, "")
	if e != nil {
		return scenarioError(e, id, "")
	}
	if (action == "run" || action == "live" || action == "validate") && !noWait {
		value, e = r.waitAuthoring(c, id, value, action != "validate")
		if e != nil {
			return scenarioError(e, id, "")
		}
	}
	return r.emit(value)
}

func (r *runner) createAuthoring(args []string) error {
	fs := flags("authoring create")
	file := fs.String("file", "", "")
	supplied := fs.String("idempotency-key", "", "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *file == "" {
		return fail("usage")
	}
	brief, e := r.readObject(*file)
	if e != nil {
		return e
	}
	key, e := replayKey(*supplied)
	if e != nil {
		return e
	}
	c, e := r.client()
	if e != nil {
		return e
	}
	r.progress("Idempotency key:", key)
	value, e := c.Request(r.ctx, "POST", "/v1/authoring-jobs", brief, key)
	if e != nil {
		return scenarioError(e, "", key)
	}
	id, ok := value["id"].(string)
	if !ok || !worldID.MatchString(id) {
		return scenarioError(fail("invalid_response"), "", key)
	}
	value["idempotency_key"] = key
	return r.emit(value)
}

func (r *runner) assignAuthoring(id string, args []string) error {
	fs := flags("authoring role")
	role := fs.String("role", "", "")
	provider := fs.String("provider", "", "")
	model := fs.String("model", "", "")
	session := fs.String("session", "", "")
	path := fs.String("token-file", "", "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *path == "" {
		return fail("usage")
	}
	if *role != "writer" && *role != "test-writer" && *role != "auditor" {
		return fail("invalid_role")
	}
	if strings.TrimSpace(*provider) == "" || strings.TrimSpace(*model) == "" || strings.TrimSpace(*session) == "" {
		return fail("invalid_role_metadata")
	}
	c, e := r.client()
	if e != nil {
		return e
	}
	// Reserve a new path before issuing credentials. O_EXCL also refuses a
	// pre-existing symlink, so another credential file is never overwritten.
	file, e := os.OpenFile(*path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return scenarioError(fail("token_file_create_failed"), id, "")
	}
	owned, e := file.Stat()
	keep := false
	defer func() {
		file.Close()
		if !keep && owned != nil {
			if current, err := os.Lstat(*path); err == nil && os.SameFile(owned, current) {
				os.Remove(*path)
			}
		}
	}()
	if e != nil || file.Chmod(0600) != nil {
		return scenarioError(fail("token_file_write_failed"), id, "")
	}
	payload := map[string]any{"role": *role, "provider": *provider, "model": *model, "session_id": *session}
	value, e := c.Request(r.ctx, "POST", "/v1/authoring-jobs/"+id+"/roles", payload, "")
	if e != nil {
		return scenarioError(e, id, "")
	}
	token, ok := value["token"].(string)
	if !ok || !roleToken.MatchString(token) {
		return scenarioError(fail("invalid_response"), id, "")
	}
	if _, e = file.WriteString(token + "\n"); e != nil {
		return scenarioError(fail("token_file_write_failed"), id, "")
	}
	if file.Sync() != nil || file.Close() != nil {
		return scenarioError(fail("token_file_write_failed"), id, "")
	}
	if current, err := os.Lstat(*path); err != nil || !os.SameFile(owned, current) {
		return scenarioError(fail("token_file_write_failed"), id, "")
	}
	keep = true
	metadata := map[string]any{"token_file": *path}
	for _, field := range []string{"id", "role", "provider", "model", "session_id"} {
		metadata[field] = value[field]
	}
	return r.emit(metadata)
}

func (r *runner) waitAuthoring(c *api.Client, id string, value map[string]any, run bool) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	last := ""
	for {
		if value["id"] != id {
			return nil, fail("invalid_response")
		}
		state, _ := value["state"].(string)
		if state == "accepted" || (!run && (state == "awaiting_audit" || state == "ready_for_live")) {
			return value, nil
		}
		if state == "rejected" || state == "validation_failed" || state == "live_failed" {
			return nil, fail("authoring_" + state)
		}
		if progress, ok := value["runner"].(map[string]any); run && ok && progress["state"] == "failed" {
			return nil, fail("authoring_run_failed")
		}
		switch state {
		case "awaiting_seed", "awaiting_tests", "ready_for_validation", "validating", "awaiting_audit", "ready_for_live", "live_evaluating":
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
			return nil, &failure{Code: "authoring_wait_timeout", Outcome: "unknown"}
		case <-time.After(500 * time.Millisecond):
		}
		var e error
		value, e = c.Request(ctx, "GET", "/v1/authoring-jobs/"+id, nil, "")
		if e != nil {
			if ctx.Err() != nil && r.ctx.Err() == nil {
				return nil, &failure{Code: "authoring_wait_timeout", Outcome: "unknown"}
			}
			return nil, e
		}
	}
}
