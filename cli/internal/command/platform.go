package command

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vlno-ai/sdk/cli/internal/api"
	"github.com/vlno-ai/sdk/cli/internal/config"
)

var productRunID = regexp.MustCompile(`^cw_[a-f0-9]{32}$`)
var customerKey = regexp.MustCompile(`^vlno_live_[A-Za-z0-9_-]{23,119}$`)
var productRevision = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/@:+-]{0,127}$`)

func validProductRun(v map[string]any, id string) bool {
	if v["id"] != id || v["engine"] != "closed_world" {
		return false
	}
	if v["state"] == "provisioning" {
		return v["result"] == nil
	}
	if !validRun(v, id) {
		return false
	}
	result, ok := v["result"].(map[string]any)
	if !ok || result["schema"] != "vlno.world-run/1" || result["engine"] != "closed_world" || result["executionState"] != v["state"] {
		return false
	}
	switch result["evaluationStatus"] {
	case "not_evaluated", "passed", "failed", "inconclusive", "invalid":
	default:
		return false
	}
	cases, ok := result["cases"].([]any)
	if !ok || len(cases) == 0 || len(cases) > 16 {
		return false
	}
	for i, raw := range cases {
		c, ok := raw.(map[string]any)
		if !ok || c["index"] != json.Number(strconv.Itoa(i)) {
			return false
		}
		scenario, ok := c["scenario"].(string)
		if !ok || !versionRef.MatchString(scenario) {
			return false
		}
	}
	return true
}

func (r *runner) platform(args []string) error {
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
				result := value["result"].(map[string]any)
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

// Claims survive failed requests. Existing files must be private regular files;
// a new claim is flushed before the worker can observe it. Never print the claim.
func productClaim(path string, create bool) (string, error) {
	if create {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			defer f.Close()
			claim, e := replayKey("")
			if e != nil {
				os.Remove(path)
				return "", e
			}
			_, e = f.WriteString(claim + "\n")
			if e != nil || f.Sync() != nil || f.Close() != nil {
				os.Remove(path)
				return "", fail("claim_file_unavailable")
			}
			return claim, nil
		}
		if !os.IsExist(err) {
			return "", fail("claim_file_unavailable")
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 130 {
		return "", fail("invalid_claim_file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fail("invalid_claim_file")
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) || actual.Mode().Perm()&0077 != 0 {
		return "", fail("invalid_claim_file")
	}
	data, err := io.ReadAll(io.LimitReader(f, 131))
	claim := strings.TrimSpace(string(data))
	if err != nil || len(data) > 130 || !roleToken.MatchString(claim) {
		return "", fail("invalid_claim_file")
	}
	return claim, nil
}

func productPause(ctx context.Context) error {
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

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

// Reconstruct an allowlisted manifest, resolving only fixed gateway paths against
// the configured origin. Server fields cannot add an owner key or redirect URLs.
func productConnection(a, run map[string]any, s config.Config) (map[string]any, error) {
	invalid := func() (map[string]any, error) { return nil, fail("invalid_connection_manifest") }
	world, ok := a["worldId"].(string)
	if !ok || !worldID.MatchString(world) {
		return invalid()
	}
	task, ok := a["task"].(string)
	if !ok {
		return invalid()
	}
	index, ok := a["index"].(json.Number)
	if !ok {
		return invalid()
	}
	n, err := index.Int64()
	if err != nil || n < 0 || n > 15 {
		return invalid()
	}
	result, ok := run["result"].(map[string]any)
	if !ok {
		return invalid()
	}
	cases, ok := result["cases"].([]any)
	if !ok || int(n) >= len(cases) {
		return invalid()
	}
	record, ok := cases[n].(map[string]any)
	if !ok || record["scenario"] != a["scenario"] || record["state"] != "running" || run["state"] != "running" {
		return invalid()
	}
	expiry, ok := a["expiresAt"].(json.Number)
	if !ok {
		return invalid()
	}
	expires, err := expiry.Float64()
	if err != nil || expires <= float64(time.Now().Unix()) {
		return invalid()
	}
	manifest, ok := a["connection"].(map[string]any)
	if !ok || manifest["schema_version"] != json.Number("1") || manifest["generation"] != json.Number("1") || manifest["world_id"] != world || manifest["task"] != task || manifest["expires_at"] != expiry {
		return invalid()
	}
	clean := map[string]any{"schema_version": 1, "generation": 1, "world_id": world, "task": task, "expires_at": expiry, "ca_file": s.CAFile}
	token := ""
	for name, suffix := range map[string]string{"mcp": "mcp", "app": "call", "environment": "exec"} {
		entry, ok := manifest[name].(map[string]any)
		if !ok {
			if name == "environment" && manifest[name] == nil {
				continue
			}
			return invalid()
		}
		route := "/v1/world-agent/" + world + "/" + suffix
		if entry["path"] != route {
			return invalid()
		}
		if _, exists := entry["url"]; exists {
			return invalid()
		}
		headers, ok := entry["headers"].(map[string]any)
		if !ok || len(headers) != 1 {
			return invalid()
		}
		auth, ok := headers["Authorization"].(string)
		if !ok || !strings.HasPrefix(auth, "Bearer ") || !roleToken.MatchString(auth[7:]) || auth[7:] == s.APIKey || (token != "" && auth != token) {
			return invalid()
		}
		token = auth
		field := map[string]any{"url": strings.TrimRight(s.Endpoint, "/") + route, "headers": map[string]string{"Authorization": auth}}
		if name == "mcp" {
			if entry["transport"] != "streamable-http" {
				return invalid()
			}
			field["transport"] = "streamable-http"
		}
		if name == "environment" {
			field["transport"] = "exec-v1"
			field["cwd"] = "/home/model"
			field["network"] = "isolated-world"
		}
		clean[name] = field
	}
	return clean, nil
}
