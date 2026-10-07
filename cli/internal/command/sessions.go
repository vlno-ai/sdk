package command

import (
	"context"
	"fmt"
	"github.com/vlno-ai/sdk/cli/internal/assessment"
	"github.com/vlno-ai/sdk/cli/internal/config"
	"github.com/vlno-ai/sdk/cli/internal/session"
	"github.com/vlno-ai/sdk/cli/internal/sessionflow"
	"os"
	"path/filepath"
	"strings"
)

type environmentKeys []string

func (v *environmentKeys) String() string     { return strings.Join(*v, ",") }
func (v *environmentKeys) Set(s string) error { *v = append(*v, s); return nil }
func (r *runner) sessions(args []string) error {
	if len(args) < 2 {
		return fail("usage")
	}
	action, path := args[0], args[1]
	if action != "start" && action != "resume" && action != "status" && action != "cancel" {
		return fail("usage")
	}
	settings, e := r.settings()
	if e != nil {
		return e
	}
	if !customerKey.MatchString(settings.APIKey) {
		return fail("invalid_customer_key")
	}
	client, e := r.client()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	var store *session.Store
	var timeout int
	if action == "start" {
		store, timeout, e = r.createSession(ctx, client, settings, args[1:])
	} else {
		if len(args) != 2 {
			return fail("usage")
		}
		store, e = session.Open(path, false)
	}
	if e != nil {
		return sessionFailure(e)
	}
	defer store.Close()
	p := &sessionflow.Protocol{

		Store: store,

		Client: client,

		Endpoint: settings.Endpoint,

		Bindings: sessionflow.Bindings{

			ValidRun: validProductRun,

			Terminal: terminalRun,

			Connection: func(a, run map[string]any) (map[string]any, error) { return productConnection(a, run, settings) },
		},
	}
	switch action {
	case "start":
		e = p.Admit(ctx)
		var handoff map[string]any
		if e == nil {
			handoff, e = p.Claim(ctx, true)
		}
		if e == nil && handoff != nil {
			token := scopedSecret(handoff)
			e = p.Execute(ctx, handoff, timeout, []string{settings.APIKey, token})
		} else if e == nil {
			e = p.Refresh(ctx)
		}
	case "resume":
		e = p.Resume(ctx)
	case "status":
		j, readErr := store.Read()
		if readErr != nil {
			e = readErr
		} else if j["admission"].(map[string]any)["state"] == "confirmed" {
			e = p.Refresh(ctx)
		}
	case "cancel":
		e = p.Cancel(ctx, true)
	}
	report, readErr := p.Report()
	if readErr == nil {
		if outputErr := r.showSession(path, report); outputErr != nil && e == nil {
			e = outputErr
		}
	} else if e == nil {
		e = readErr
	}
	if e != nil {
		if !r.opts.json {
			fmt.Fprintln(r.err, "The operation could not be confirmed. Keep the private session directory for recovery.")
		}
		return sessionFailure(e)
	}
	return nil
}
func (r *runner) createSession(ctx context.Context, client sessionflow.Client, s config.Config, args []string) (*session.Store, int, error) {
	fs := flags("sessions start")
	id := fs.String("assessment", "", "")
	revision := fs.String("revision", "", "")
	cwd := fs.String("cwd", "", "")
	seconds := fs.Int("agent-timeout", 300, "")
	var keys environmentKeys
	fs.Var(&keys, "env", "")
	if len(args) < 2 || !hasCommandSeparator(args[1:]) || fs.Parse(args[1:]) != nil || fs.NArg() == 0 ||
		!assessment.UUID.MatchString(*id) ||
		!assessment.UUID.MatchString(*revision) ||
		*seconds < 1 ||
		*seconds > 3600 {
		return nil, 0, fail("usage")
	}
	if _, e := os.Lstat(args[0]); !os.IsNotExist(e) {
		return nil, 0, fail("session_directory_exists")
	}
	dir, e := filepath.Abs(*cwd)
	if e != nil {
		return nil, 0, fail("invalid_cwd")
	}
	argv := fs.Args()
	for _, arg := range argv {
		if strings.Contains(arg, s.APIKey) {
			return nil, 0, fail("session_command_contains_credential")
		}
	}
	who, e := client.Request(ctx, "GET", "/v1/assessment-runs/access", nil, "")
	if e != nil {
		return nil, 0, e
	}
	prep, e := client.Request(ctx, "GET", "/v1/assessments/"+*id+"/revisions/"+*revision+"/run-preparation", nil, "")
	if e != nil {
		return nil, 0, e
	}
	if !assessment.Preparation(prep, *id, *revision) {
		return nil, 0, fail("invalid_assessment_response")
	}
	argsAny, keysAny := []any{}, []any{}
	for _, v := range argv {
		argsAny = append(argsAny, v)
	}
	for _, v := range keys {
		keysAny = append(keysAny, v)
	}
	journal, claim, e := session.Initial(s.Endpoint, who, prep, map[string]any{

		"argv": argsAny,

		"envKeys": keysAny,

		"cwd": dir,
	})
	if e != nil {
		return nil, 0, e
	}
	store, e := session.Create(args[0], journal, claim)
	return store, *seconds, e
}
func scopedSecret(m map[string]any) string {
	entry, _ := m["mcp"].(map[string]any)
	headers, _ := entry["headers"].(map[string]string)
	return strings.TrimPrefix(headers["Authorization"], "Bearer ")
}

func hasCommandSeparator(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return true
		}
	}
	return false
}
