package command

import (
	"encoding/json"
	"strings"
)

func (r *runner) worlds(args []string) error {
	if len(args) == 0 {
		return fail("usage")
	}
	action := args[0]
	if action == "create" {
		return r.create(args[1:])
	}
	switch action {
	case "status", "reset", "destroy", "operations", "setup", "seed", "call", "snapshot", "evidence", "task", "evaluate", "trace", "exec":
	case "connect":
		return r.connect(args[1:])
	default:
		return fail("usage")
	}
	if len(args) < 2 || !worldID.MatchString(args[1]) {
		return fail("invalid_world_id")
	}
	id, tail := args[1], args[2:]
	path := "/v1/worlds/" + id
	method := "GET"
	var payload any
	key, target := "", ""
	fs := flags(action)
	var noWait *bool
	switch action {
	case "exec":
		cwd := fs.String("cwd", "/home/model", "")
		seconds := fs.Int("exec-timeout", 30, "")
		if fs.Parse(tail) != nil || fs.NArg() == 0 || *seconds < 1 || *seconds > 60 {
			return fail("usage")
		}
		method, path, payload = "POST", path+"/exec", map[string]any{"argv": fs.Args(), "cwd": *cwd, "timeout_seconds": *seconds}
	case "reset", "destroy":
		noWait = fs.Bool("no-wait", false, "")
		var supplied *string
		if action == "reset" {
			supplied = fs.String("idempotency-key", "", "")
		}
		if fs.Parse(tail) != nil || fs.NArg() != 0 {
			return fail("usage")
		}
		if action == "reset" {
			var e error
			key, e = replayKey(*supplied)
			if e != nil {
				return e
			}
			method, path, payload, target = "POST", path+"/reset", map[string]any{}, "ready"
		} else {
			method, target = "DELETE", "closed"
		}
	case "setup":
		login := fs.String("login", "", "")
		email := fs.String("email", "", "")
		alias := fs.String("alias", "primary", "")
		if fs.Parse(tail) != nil || fs.NArg() != 0 || *login == "" || *email == "" {
			return fail("usage")
		}
		method, path, payload = "POST", path+"/setup", map[string]any{"identity": map[string]string{"login": *login, "email": *email}, "alias": *alias}
	case "seed", "call":
		if len(tail) == 0 || strings.HasPrefix(tail[0], "-") {
			return fail("usage")
		}
		op := tail[0]
		raw := fs.String("args", "", "")
		file := fs.String("args-file", "", "")
		var alias *string
		if action == "seed" {
			alias = fs.String("alias", "primary", "")
		}
		if fs.Parse(tail[1:]) != nil || fs.NArg() != 0 || (*raw != "" && *file != "") {
			return fail("usage")
		}
		arguments := map[string]any{}
		if *file != "" {
			v, e := r.readObject(*file)
			if e != nil {
				return e
			}
			arguments = v
		} else if *raw != "" {
			v, e := decodeObject(strings.NewReader(*raw))
			if e != nil {
				return e
			}
			arguments = v
		}
		body := map[string]any{"operation": op, "arguments": arguments}
		route := "call"
		if action == "seed" {
			route = "fixtures"
			body["alias"] = *alias
		}
		method, path, payload = "POST", path+"/"+route, body
	default:
		if len(tail) != 0 {
			return fail("usage")
		}
		if action != "status" {
			path += "/" + action
		}
		if action == "evaluate" {
			method, payload = "POST", map[string]any{}
		}
	}
	c, e := r.client()
	if e != nil {
		return e
	}
	if key != "" {
		r.progress("Idempotency key:", key)
	}
	v, e := c.Request(r.ctx, method, path, payload, key)
	if e != nil {
		return lifecycleError(e, id, key)
	}
	if target != "" && !*noWait {
		r.progress("World:", id)
		v, e = r.wait(c, id, target, v)
		if e != nil {
			return lifecycleError(e, id, key)
		}
	}
	if key != "" {
		v["idempotency_key"] = key
	}
	if action == "evaluate" {
		status, _ := v["status"].(string)
		switch status {
		case "passed", "failed", "inconclusive", "invalid":
		default:
			return fail("invalid_response")
		}
		if e := r.emit(public(v)); e != nil {
			return e
		}
		if status != "passed" {
			return evaluationUnpassed
		}
		return nil
	}
	if action == "exec" {
		code, ok := v["exit_code"].(json.Number)
		number, err := code.Int64()
		truncated, valid := v["truncated"].(bool)
		if !ok || err != nil || number < 0 || number > 255 || !valid {
			return fail("invalid_response")
		}
		if err := r.emit(v); err != nil {
			return err
		}
		if number != 0 || truncated {
			return executionUnpassed
		}
		return nil
	}
	return r.emit(public(v))
}
