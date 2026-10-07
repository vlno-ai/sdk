package sessionflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/vlno-ai/sdk/cli/internal/harness"
	"github.com/vlno-ai/sdk/cli/internal/session"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Execute is called only by the explicit start path, never by Resume.
func (p *Protocol) Execute(ctx context.Context, handoff map[string]any, seconds int, secrets []string) error {
	j, e := p.read()
	if e != nil {
		return e
	}
	execution := object(j, "execution")
	if execution["state"] != "not_started" || execution["mode"] != "supervised" || seconds < 1 || seconds > 3600 {
		return code("session_launch_forbidden")
	}
	c := object(j, "claim")
	if c["state"] != "ready" {
		return code("session_case_unavailable")
	}
	if _, e = p.identity(ctx, j, c["actor"]); e != nil {
		return e
	}
	expires, e := time.Parse("2006-01-02T15:04:05.000Z", text(object(c, "case")["expiresAt"]))
	if e != nil || !expires.After(time.Now()) {
		return code("connection_expired")
	}
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	if expires.Before(deadline) {
		deadline = expires
	}
	claim, e := p.Store.PrivateClaim()
	if e != nil {
		return e
	}
	secrets = append(append([]string{}, secrets...), claim)
	command := object(execution, "command")
	argv := stringsOf(command["argv"])
	keys := stringsOf(command["envKeys"])
	for _, arg := range argv {
		for _, secret := range secrets {
			if secret != "" && strings.Contains(arg, secret) {
				return code("session_command_contains_credential")
			}
		}
	}
	if e = p.Record("lifecycle", map[string]any{

		"event": "capture_started",

		"streams": []any{"stdout", "stderr"},

		"agentTimeoutSeconds": json.Number(strconv.Itoa(seconds)),
	}, secrets, false); e != nil {
		return e
	}
	if e = p.Record("message", map[string]any{"role": "user", "content": handoff["task"]}, secrets, false); e != nil {
		return e
	}
	if e = p.Record("shell_command", map[string]any{"argv": command["argv"], "location": "customer harness"}, secrets, false); e != nil {
		return e
	}
	if e = p.Flush(ctx); e != nil {
		return e
	}
	payload, e := json.Marshal(map[string]any{"task": handoff["task"], "connection": handoff})
	if e != nil {
		return code("invalid_connection_manifest")
	}
	launch, e := session.NewID()
	if e != nil {
		return e
	}
	if _, e = p.identity(ctx, j, c["actor"]); e != nil {
		return e
	}
	if e = p.Store.Update(func(j map[string]any) error {
		x := object(j, "execution")
		x["state"] = "start_unknown"
		x["launchId"] = launch
		return nil
	}); e != nil {
		return e
	}
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	capture := &capture{

		p: p,

		ctx: runCtx,

		stop: cancel,

		secrets: secrets,
	}
	stdout, stderr := &stream{capture: capture, kind: "stdout"}, &stream{capture: capture, kind: "stderr"}
	result := harness.Run(runCtx, harness.Spec{

		Argv: argv,

		Env: harness.Environment(keys, secrets...),

		Cwd: text(command["cwd"]),

		Input: payload,

		Stdout: stdout,

		Stderr: stderr,
	}, func(pid int) error {
		capture.mu.Lock()
		defer capture.mu.Unlock()
		return p.Store.Update(func(j map[string]any) error {
			x := object(j, "execution")
			x["state"] = "running"
			x["pid"] = json.Number(strconv.Itoa(pid))
			return nil
		})
	})
	if isFilesystemFailure(result.Err) || isFilesystemFailure(capture.failure()) {
		return code("session_storage_uncertain")
	}
	if e = p.Store.Update(func(j map[string]any) error {
		x := object(j, "execution")
		if result.ExitObserved && x["state"] == "running" {
			x["state"] = "exited"
			x["exitCode"] = json.Number(strconv.Itoa(result.ExitCode))
			x["finishedAt"] = now()
		} else {
			x["state"] = "exit_unknown"
		}
		return nil
	}); e != nil {
		return e
	}
	captureErr := capture.finish(stdout, stderr)
	var exited *exec.ExitError
	complete := captureErr == nil && result.ExitObserved && result.Status != "timeout" && (result.Err == nil || errors.As(result.Err, &exited))
	if complete {
		e = p.Record("lifecycle", map[string]any{

			"event": "capture_finished",

			"exitCode": json.Number(strconv.Itoa(result.ExitCode)),

			"streams": []any{"stdout", "stderr"},
		}, secrets, true)
		if e == nil {
			e = p.Flush(ctx)
		}
		if e == nil {
			e = p.Complete()
		}
		if e != nil {
			complete = false
			captureErr = e
		}
	}
	if !complete {
		if e = p.Gap("capture_incomplete"); e != nil {
			return e
		}
	}
	status := result.Status
	if captureErr != nil && errors.Is(runCtx.Err(), context.Canceled) {
		status = "error"
	}
	if !complete && status == "completed" {
		status = "error"
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if captureErr != nil {
		if e = p.PrepareFinish(ctx, status); e != nil {
			return e
		}
		return captureErr
	}
	if e = p.Finish(ctx, status, true); e != nil {
		return e
	}
	return p.Observe(ctx)
}
