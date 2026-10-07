package command

import (
	"context"
	"regexp"
)

func (r *runner) platform(args []string) error {
	if len(args) > 0 && args[0] == "assessments" {
		return r.assessments(args[1:])
	}
	if len(args) > 0 && args[0] == "data" {
		return r.productData(args[1:])
	}
	if len(args) == 2 && args[0] == "suites" && args[1] == "list" {
		return r.productSuites()
	}
	if len(args) > 0 && args[0] == "login" {
		return r.saveLogin(args[1:], true)
	}
	if len(args) < 3 || args[0] != "runs" {
		return fail("usage")
	}
	action, id := args[1], args[2]
	if action == "record" || action == "transcript" {
		return r.productTranscript(action, id, args[3:])
	}
	switch action {
	case "create", "status", "next", "finish", "wait", "watch", "cancel", "evidence", "artifact":
	default:
		return fail("usage")
	}
	if action != "create" && !productRunID.MatchString(id) {
		return fail("invalid_run_id")
	}
	fs := flags("platform runs")
	var body any
	key, claimPath, outPath := "", "", ""
	artifactIndex, kind := -1, ""
	var after int64
	switch action {
	case "watch":
		fs.Int64Var(&after, "after", 0, "")
		if fs.Parse(args[3:]) != nil || fs.NArg() != 0 || after < 0 || after > 9007199254740991 {
			return fail("usage")
		}
	case "artifact":
		fs.IntVar(&artifactIndex, "case", -1, "")
		fs.StringVar(&kind, "kind", "", "")
		fs.StringVar(&outPath, "out", "", "")
		if fs.Parse(args[3:]) != nil || fs.NArg() != 0 || artifactIndex < 0 || artifactIndex > 15 || !artifactKind.MatchString(kind) || outPath == "" || outPath == "-" {
			return fail("usage")
		}
	case "create":
		revision := fs.String("revision", "", "")
		replay := fs.String("idempotency-key", "", "")
		ttl := fs.Int("ttl", 1800, "")
		environment := fs.Bool("environment", false, "")
		if fs.Parse(args[3:]) != nil || fs.NArg() != 0 {
			return fail("usage")
		}
		if !versionRef.MatchString(id) {
			return fail("invalid_suite_ref")
		}
		if !productRevision.MatchString(*revision) {
			return fail("invalid_revision")
		}
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`).MatchString(*replay) {
			return fail("invalid_idempotency_key")
		}
		if *ttl < 60 || *ttl > 3600 {
			return fail("invalid_ttl")
		}
		key = *replay
		body = map[string]any{"suite": id, "revision": *revision, "ttl_seconds": *ttl, "environment": *environment}
	case "next":
		fs.StringVar(&claimPath, "claim-file", "", "")
		fs.StringVar(&outPath, "out", "", "")
		if fs.Parse(args[3:]) != nil || fs.NArg() != 0 || claimPath == "" || outPath == "" || outPath == "-" || claimPath == outPath {
			return fail("usage")
		}
	case "finish":
		fs.StringVar(&claimPath, "claim-file", "", "")
		index := fs.Int("case", -1, "")
		status := fs.String("agent-status", "completed", "")
		if fs.Parse(args[3:]) != nil || fs.NArg() != 0 || claimPath == "" || *index < 0 || *index > 15 {
			return fail("usage")
		}
		if *status != "completed" && *status != "error" && *status != "timeout" {
			return fail("invalid_agent_status")
		}
		claim, err := productClaim(claimPath, false)
		if err != nil {
			return err
		}
		body = map[string]any{"case_index": *index, "claim": claim, "agent_status": *status}
	default:
		if len(args) != 3 {
			return fail("usage")
		}
	}
	settings, err := r.settings()
	if err != nil {
		return err
	}
	if !customerKey.MatchString(settings.APIKey) {
		return fail("invalid_customer_key")
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	if action == "evidence" || action == "artifact" {
		return r.productEvidence(c, id, artifactIndex, kind, outPath)
	}
	if action == "watch" {
		return r.productWatch(c, id, after)
	}
	if action == "next" {
		return r.productNext(c, settings, id, claimPath, outPath)
	}
	path, method := "/v1/world-runs/"+id, "GET"
	if action == "create" {
		path, method = "/v1/world-runs", "POST"
		id = ""
	}
	if action == "finish" || action == "cancel" {
		path += "/" + action
		method = "POST"
	}
	if action == "cancel" {
		body = map[string]any{}
	}
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	for {
		value, err := c.Request(ctx, method, path, body, key)
		if err != nil {
			return runError(err, id, key)
		}
		if action == "create" {
			id, _ = value["id"].(string)
		}
		if !productRunID.MatchString(id) || !validProductRun(value, id) {
			return runError(fail("invalid_response"), id, key)
		}
		if action == "wait" && !terminalRun(value) {
			value, err = r.productWait(ctx, c, id)
			if err != nil {
				return runError(err, id, key)
			}
		}
		if action != "wait" || terminalRun(value) {
			if err = r.emit(value); err != nil {
				return err
			}
			if action == "wait" {
				result, _ := value["result"].(map[string]any)
				summary, _ := result["summary"].(map[string]any)
				cleanup, _ := summary["cleanup"].(map[string]any)
				if value["state"] != "completed" || result["evaluationStatus"] != "passed" || cleanup["status"] != "confirmed" {
					return evaluationUnpassed
				}
			}
			return nil
		}
		if err = productPause(ctx); err != nil {
			return runError(err, id, key)
		}
	}
}
