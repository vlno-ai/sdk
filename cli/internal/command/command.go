// Package command implements the customer CLI over the versioned worker API.
package command

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/vlno-ai/sdk/cli/internal/api"
	"github.com/vlno-ai/sdk/cli/internal/config"
)

const Version = "0.14.0"
const help = `vlno — remote world environments (preview)

Usage:
  vlno login --key-stdin
  vlno platform login --key-stdin
  vlno platform data policy
  vlno platform data set-policy --days <7|30|90|keep> --revision <current-revision>
  vlno platform data status <cw_run-id>
  vlno platform data delete <cw_run-id> --confirm-run <same-cw_run-id>
  vlno platform data deletions [--limit 50] [--offset 0]
  vlno platform suites list
  vlno platform runs create <suite-ref> --revision <label> --idempotency-key <key> [--environment] [--ttl 1800]
  vlno platform runs status <cw_run-id>
  vlno platform runs next <cw_run-id> --claim-file <private-file> --out <new-private-file>
  vlno platform runs finish <cw_run-id> --case <index> --claim-file <private-file> [--agent-status completed]
  vlno platform runs wait <cw_run-id>
  vlno platform runs watch <cw_run-id> [--after <cursor>]
  vlno platform runs cancel <cw_run-id>
  vlno platform runs transcript <cw_run-id> --case <index> [--after <cursor>]
  vlno platform runs record <cw_run-id> --case <index> --claim-file <private-file>
  vlno platform runs evidence <cw_run-id>
  vlno platform runs artifact <cw_run-id> --case <index> --kind <report|snapshot|evidence|trace> --out <new-private-file>
  vlno logout
  vlno apps validate --file <app.json>
  vlno apps import --file <app.json> [--idempotency-key <key>] [--no-wait]
  vlno apps status <job-id>
  vlno suites run <suite-ref> --revision <label> (--agent-config <file> | --harness <alias>) [--environment] [--case-timeout 300s]
  vlno runs status <run-id>
  vlno runs cancel <run-id>
  vlno runs compare <baseline-id> <candidate-id>
  vlno catalog harnesses [show <alias>]
  vlno catalog apps [show <app>]
  vlno catalog worlds [show <ref>] [--app <app>]
  vlno catalog scenarios [show <ref>] [--world <ref>]
  vlno catalog families [show <family>]
  vlno catalog suites [show <ref>]
  vlno scenarios generate --family <family> --template <ref> --seed <integer>
  vlno scenarios status <job-id>
  vlno scenarios freeze <job-id> --suite <ref>
  vlno authoring create --file <brief.json> [--idempotency-key <key>]
  vlno authoring status <job-id>
  vlno authoring context <job-id>
  vlno authoring run <job-id> [--no-wait]
  vlno authoring live <job-id> [--no-wait]
  vlno authoring validate <job-id> [--no-wait]
  vlno authoring freeze <job-id> --suite <ref>
  vlno authoring role <job-id> --role <role> --provider <name> --model <model> --session <id> --token-file <new-file>
  vlno authoring submit <job-id> --file <artifact.json> --basis <sha256>
  vlno worlds create --app <app> [--ttl 900] [--mapping <file>]
  vlno worlds create --template <ref> [--fixture <ref>] [--ttl 900] [--environment]
  vlno worlds status <id>
  vlno worlds connect <id> --out <new-private-file>
  vlno mcp --connection <private-file>
  vlno worlds trace <id>
  vlno worlds exec <id> --cwd /home/model --exec-timeout 30 -- <program> [args...]
  vlno worlds task <id>
  vlno worlds evaluate <id>
  vlno worlds reset <id>
  vlno worlds destroy <id>
  vlno worlds operations <id>
  vlno worlds setup <id> --login <login> --email <email> [--alias primary]
  vlno worlds seed <id> <operation> [--args '<json>'] [--alias primary]
  vlno worlds call <id> <operation> [--args '<json>']
  vlno worlds snapshot <id>
  vlno worlds evidence <id>
  vlno version

Global flags (accepted before or after the command):
  --endpoint <origin>       Worker or VLNO product API HTTP(S) origin
  --ca-file <file>          Private CA certificate
  --config <file>           Private configuration file
  --json                   Machine-readable stdout and errors
  --timeout <duration>      Per-request timeout (default 200s)
  --wait-timeout <duration> Lifecycle wait deadline (default 330s)

Connection settings: flags > VLNO_ENDPOINT/VLNO_CA_FILE > saved config.
Credentials: VLNO_API_KEY > saved config. Login reads a key only from stdin.
VLNO_CONFIG selects a config file. Login uses an existing worker API key.
Create/reset: --idempotency-key <key> enables explicit replay.
Create/reset/destroy wait by default; --no-wait returns accepted state.
Seed/call also accept --args-file <file>, or '-' for stdin.
Human output includes progress; JSON output includes lifecycle replay keys.
Catalog includes imported apps and operator-published versioned worlds/scenarios.
Evaluate prints its report; passed exits 0, failed/inconclusive/invalid exit 1.
Generate supports --count 1..4, --distractors 1..4 (both default 2), --no-wait,
and --idempotency-key. Validation uses fresh worlds and can exceed 330s;
increase --wait-timeout or resume inspection with scenarios status <job-id>.
Generation is deterministic built-in workflow variation, not model generation.
Authoring run uses the operator-configured provider; no provider key is submitted
in a job. Role files are exclusive private files; bearer credentials never print.
Authoring submit requires a role credential via the normal credential settings.
Authoring run waits for acceptance; validate waits for the audit stage.
Acceptance includes the required honest-model live gate; live starts that gate
after manually submitted audits have passed.
Apps validate checks structure only; import runs the contract and publishes on success.
source_image must be an immutable sha256 image ID already on the worker.
Import never uploads or pulls an image. Increase --wait-timeout for import validation.
Connect exports scoped HTTP MCP/app connections to a new private file.
Platform commands use a customer API key against VLNO's organization-owned API.
Platform next creates/reuses a private claim file and waits for a ready connection.
Use a NEW claim file per case; reuse it only to recover that same assignment.
Pass only the connection file to the harness, never the customer key or claim file.
Platform record streams UTF-8 stdin to the live trajectory (native JSON is retained).
Use the SDK process recorder to capture stdout and stderr separately.
Platform next and wait use SSE and resume read-only streams after disconnects.
Platform watch streams run status and recorded trajectory; --json emits JSON lines.
Platform finish starts grading; wait exits 0 only for a passed, cleaned-up run.
Create --environment opts into the isolated workstation exec endpoint and MCP tool.
Exec returns a structured result; guest failure/truncation exits 1. Image upload is pending.
`

// An evaluation outcome is already printed as a report, not an API error.
var evaluationUnpassed = errors.New("evaluation_not_passed")
var executionUnpassed = errors.New("execution_not_passed")

type failure struct {
	Code    string `json:"code"`
	Status  int    `json:"status,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Stage   string `json:"stage,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	WorldID string `json:"world_id,omitempty"`
	JobID   string `json:"job_id,omitempty"`
	RunID   string `json:"run_id,omitempty"`
	Key     string `json:"idempotency_key,omitempty"`
}

func (f *failure) Error() string { return f.Code }
func fail(code string) error     { return &failure{Code: code} }

type options struct {
	endpoint, caFile, configPath string
	json                         bool
	timeout, waitTimeout         time.Duration
}

type runner struct {
	ctx      context.Context
	in       io.Reader
	out, err io.Writer
	opts     options
}

// Run returns an exit status; diagnostics contain no credentials or raw server errors.
func Run(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer) int {
	opts, remaining, err := globals(args)
	r := runner{ctx: ctx, in: in, out: out, err: stderr, opts: opts}
	if err == nil {
		err = r.execute(remaining)
	}
	if err == nil {
		return 0
	}
	if errors.Is(err, evaluationUnpassed) || errors.Is(err, executionUnpassed) {
		return 1
	}
	f := &failure{Code: "command_failed"}
	var own *failure
	var remote *api.Error
	if errors.As(err, &own) {
		f = own
	} else if errors.As(err, &remote) {
		f = &failure{Code: remote.Code, Status: remote.Status, Outcome: remote.Outcome, Reason: remote.Reason, Stage: remote.Stage}
	}
	if errors.Is(err, context.Canceled) {
		f.Code = "cancelled"
	}
	if opts.json {
		_ = json.NewEncoder(stderr).Encode(map[string]any{"error": f})
	} else {
		fmt.Fprintln(stderr, "Error:", f.Code)
		if f.WorldID != "" {
			fmt.Fprintln(stderr, "World:", f.WorldID)
		}
		if f.RunID != "" {
			fmt.Fprintln(stderr, "Run:", f.RunID)
		}
		if f.JobID != "" {
			fmt.Fprintln(stderr, "Job:", f.JobID)
		}
		if f.Key != "" {
			fmt.Fprintln(stderr, "Idempotency key:", f.Key)
		}
		if f.Stage != "" {
			fmt.Fprintln(stderr, "Stage:", f.Stage)
		}
		if f.Reason != "" {
			fmt.Fprintln(stderr, "Reason:", f.Reason)
		}
		if f.Outcome != "" {
			fmt.Fprintln(stderr, "Outcome:", f.Outcome)
		}
	}
	if f.Code == "cancelled" {
		return 130
	}
	if strings.HasPrefix(f.Code, "invalid_") || f.Code == "usage" || f.Code == "unsupported_feature" {
		return 2
	}
	return 1
}

func globals(args []string) (options, []string, error) {
	o := options{timeout: 200 * time.Second, waitTimeout: 330 * time.Second}
	// Recognize --json even when another argument is invalid.
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "--json" {
			o.json = true
		}
	}
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		name, value, equal := strings.Cut(args[i], "=")
		if name == "--json" {
			if equal {
				return o, nil, fail("invalid_flag")
			}
			continue
		}
		switch name {
		case "--endpoint", "--ca-file", "--config", "--timeout", "--wait-timeout":
			if !equal {
				i++
				if i >= len(args) {
					return o, nil, fail("invalid_flag")
				}
				value = args[i]
			}
			if value == "" {
				return o, nil, fail("invalid_flag")
			}
			switch name {
			case "--endpoint":
				o.endpoint = value
			case "--ca-file":
				o.caFile = value
			case "--config":
				o.configPath = value
			default:
				d, e := time.ParseDuration(value)
				if e != nil || d <= 0 {
					return o, nil, fail("invalid_timeout")
				}
				if name == "--timeout" {
					o.timeout = d
				} else {
					o.waitTimeout = d
				}
			}
		default:
			rest = append(rest, args[i])
		}
	}
	return o, rest, nil
}

func (r *runner) execute(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, e := fmt.Fprint(r.out, help)
		return e
	}
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "--help" || a == "-h" {
			_, e := fmt.Fprint(r.out, help)
			return e
		}
	}
	if args[0] == "version" || args[0] == "--version" {
		if len(args) != 1 {
			return fail("usage")
		}
		return r.emit(map[string]any{"version": Version})
	}
	// Validate top-level routes before accessing config or the network.
	switch args[0] {
	case "platform":
		return r.platform(args[1:])
	case "login":
		return r.login(args[1:])
	case "logout":
		if len(args) != 1 {
			return fail("usage")
		}
		p, e := r.configPath()
		if e != nil {
			return e
		}
		if config.Delete(p) != nil {
			return fail("config_delete_failed")
		}
		return r.emit(map[string]any{"logged_out": true})
	case "catalog":
		return r.catalog(args[1:])
	case "worlds":
		return r.worlds(args[1:])
	case "mcp":
		return r.mcp(args[1:])
	case "scenarios":
		return r.scenarios(args[1:])
	case "authoring":
		return r.authoring(args[1:])
	case "apps":
		return r.apps(args[1:])
	case "suites":
		return r.suites(args[1:])
	case "runs":
		return r.runs(args[1:])
	default:
		return fail("usage")
	}
}

func (r *runner) catalog(args []string) error {
	if len(args) == 0 {
		return fail("usage")
	}
	kind, tail := args[0], args[1:]
	fs := flags("catalog")
	var filter *string
	filterField, missing := "", ""
	switch kind {
	case "harnesses":
		missing = "harness_not_found"
	case "apps":
		missing = "app_not_found"
	case "worlds":
		filter = fs.String("app", "", "")
		filterField, missing = "app", "world_template_not_found"
	case "scenarios":
		filter = fs.String("world", "", "")
		filterField, missing = "template", "scenario_not_found"
	case "families":
		missing = "scenario_family_not_found"
	case "suites":
		missing = "suite_not_found"
	default:
		return fail("usage")
	}
	ref := ""
	if len(tail) > 0 && tail[0] == "show" {
		if len(tail) < 2 {
			return fail("usage")
		}
		ref, tail = tail[1], tail[2:]
	}
	if fs.Parse(tail) != nil {
		return fail("usage")
	}
	if fs.NArg() != 0 {
		if ref != "" || fs.NArg() != 2 || fs.Arg(0) != "show" {
			return fail("usage")
		}
		ref = fs.Arg(1)
	}
	c, e := r.client()
	if e != nil {
		return e
	}
	value, e := c.Request(r.ctx, "GET", "/v1/catalog", nil, "")
	if e != nil {
		return e
	}
	entries, ok := value[kind].(map[string]any)
	if !ok {
		return fail("invalid_response")
	}
	selected := make(map[string]any)
	for name, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			return fail("invalid_response")
		}
		if filter == nil || *filter == "" || entry[filterField] == *filter {
			selected[name] = entry
		}
	}
	if ref != "" {
		entry, ok := selected[ref]
		if !ok {
			return fail(missing)
		}
		return r.emit(entry)
	}
	if r.opts.json {
		return r.emit(map[string]any{kind: selected})
	}
	w := tabwriter.NewWriter(r.out, 0, 4, 2, ' ', 0)
	switch kind {
	case "harnesses":
		fmt.Fprintln(w, "HARNESS\tPROVIDER\tIMAGE")
	case "apps":
		fmt.Fprintln(w, "APP\tCONTRACT\tIMAGE")
	case "worlds":
		fmt.Fprintln(w, "WORLD\tAPP\tDESCRIPTION")
	case "scenarios":
		fmt.Fprintln(w, "SCENARIO\tWORLD\tDESCRIPTION")
	case "families":
		fmt.Fprintln(w, "FAMILY\tCOMPILER\tDESCRIPTION")
	case "suites":
		fmt.Fprintln(w, "SUITE\tJOB\tHASH")
	}
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := selected[name].(map[string]any)
		second, third := entry[filterField], entry["description"]
		if kind == "harnesses" {
			second, third = entry["provider"], entry["image_id"]
		} else if kind == "apps" {
			contract, _ := entry["app"].(map[string]any)
			second, third = contract["id"], entry["image_id"]
		} else if kind == "families" {
			second = entry["compiler_version"]
		} else if kind == "suites" {
			second, third = entry["job_id"], entry["suite_sha256"]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", safeText(name), safeText(fmt.Sprint(second)), safeText(fmt.Sprint(third)))
	}
	return w.Flush()
}

func (r *runner) configPath() (string, error) {
	if r.opts.configPath != "" {
		return r.opts.configPath, nil
	}
	if p := os.Getenv("VLNO_CONFIG"); p != "" {
		return p, nil
	}
	p, e := config.DefaultPath()
	if e != nil {
		return "", fail("config_path_failed")
	}
	return p, nil
}
func (r *runner) settings() (config.Config, error) {
	p, e := r.configPath()
	if e != nil {
		return config.Config{}, e
	}
	c, e := config.Load(p)
	if e != nil {
		return c, fail("config_read_failed")
	}
	if v := os.Getenv("VLNO_ENDPOINT"); v != "" {
		c.Endpoint = v
	}
	if v := os.Getenv("VLNO_API_KEY"); v != "" {
		c.APIKey = v
	}
	if v := os.Getenv("VLNO_CA_FILE"); v != "" {
		c.CAFile = v
	}
	if r.opts.endpoint != "" {
		c.Endpoint = r.opts.endpoint
	}
	if r.opts.caFile != "" {
		c.CAFile = r.opts.caFile
	}
	return c, nil
}
func (r *runner) client() (*api.Client, error) {
	c, e := r.settings()
	if e != nil {
		return nil, e
	}
	if c.APIKey == "" {
		return nil, fail("missing_credentials")
	}
	if c.Endpoint == "" {
		return nil, fail("missing_endpoint")
	}
	return api.New(c.Endpoint, c.APIKey, c.CAFile, r.opts.timeout)
}
func (r *runner) login(args []string) error {
	return r.saveLogin(args, false)
}

func (r *runner) saveLogin(args []string, platform bool) error {
	fs := flags("login")
	stdin := fs.Bool("key-stdin", false, "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || !*stdin {
		return fail("usage")
	}
	c, e := r.settings()
	if e != nil {
		return e
	}
	data, e := io.ReadAll(io.LimitReader(r.in, 131))
	if e != nil || len(data) > 130 {
		return fail("invalid_credentials")
	}
	c.APIKey = strings.TrimSpace(string(data))
	if platform && !customerKey.MatchString(c.APIKey) {
		return fail("invalid_customer_key")
	}
	if c.Endpoint == "" {
		return fail("missing_endpoint")
	}
	a, e := api.New(c.Endpoint, c.APIKey, c.CAFile, r.opts.timeout)
	if e != nil {
		return e
	}
	path := "/v1/catalog"
	if platform {
		path = "/v1/world-runs"
	}
	if _, e = a.Request(r.ctx, "GET", path, nil, ""); e != nil {
		return e
	}
	p, e := r.configPath()
	if e != nil {
		return e
	}
	if c.CAFile != "" {
		absolute, err := filepath.Abs(c.CAFile)
		if err != nil {
			return fail("invalid_ca_file")
		}
		c.CAFile = absolute
	}
	if config.Save(p, c) != nil {
		return fail("config_save_failed")
	}
	return r.emit(map[string]any{"authenticated": true, "endpoint": c.Endpoint})
}

func flags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}

var worldID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
var versionRef = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}@[1-9][0-9]{0,8}$`)

func replayKey(value string) (string, error) {
	if value != "" {
		if !keyPattern.MatchString(value) {
			return "", fail("invalid_idempotency_key")
		}
		return value, nil
	}
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", fail("random_failed")
	}
	return hex.EncodeToString(b), nil
}

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

func (r *runner) create(args []string) error {
	fs := flags("create")
	app := fs.String("app", "", "")
	template := fs.String("template", "", "")
	fixture := fs.String("fixture", "", "")
	environment := fs.Bool("environment", false, "")
	ttl := fs.Int("ttl", 900, "")
	mapping := fs.String("mapping", "", "")
	supplied := fs.String("idempotency-key", "", "")
	noWait := fs.Bool("no-wait", false, "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || (*app == "") == (*template == "") {
		return fail("usage")
	}
	if *fixture != "" && *template == "" {
		return fail("invalid_fixture")
	}
	if (*template != "" && !versionRef.MatchString(*template)) || (*fixture != "" && !versionRef.MatchString(*fixture)) {
		return fail("invalid_version_ref")
	}
	if *ttl < 60 || *ttl > 3600 {
		return fail("invalid_lease")
	}
	key, e := replayKey(*supplied)
	if e != nil {
		return e
	}
	payload := map[string]any{"ttl_seconds": *ttl}
	if *environment {
		payload["environment"] = true
	}
	if *app != "" {
		payload["image"] = *app
	} else {
		payload["template"] = *template
	}
	if *fixture != "" {
		payload["fixture"] = *fixture
	}
	if *mapping != "" {
		v, e := r.readObject(*mapping)
		if e != nil {
			return e
		}
		payload["mapping"] = v
	}
	c, e := r.client()
	if e != nil {
		return e
	}
	r.progress("Idempotency key:", key)
	v, e := c.Request(r.ctx, "POST", "/v1/worlds", payload, key)
	if e != nil {
		return lifecycleError(e, "", key)
	}
	id, ok := v["id"].(string)
	if !ok || !worldID.MatchString(id) {
		return lifecycleError(fail("invalid_response"), "", key)
	}
	r.progress("World:", id)
	if !*noWait {
		v, e = r.wait(c, id, "ready", v)
		if e != nil {
			return lifecycleError(e, id, key)
		}
	}
	if key != "" {
		v["idempotency_key"] = key
	}
	return r.emit(public(v))
}

func lifecycleError(e error, id, key string) error {
	f := &failure{Code: "command_failed", WorldID: id, Key: key}
	var a *api.Error
	var own *failure
	if errors.As(e, &a) {
		f.Code = a.Code
		f.Status = a.Status
		f.Outcome = a.Outcome
		f.Reason = a.Reason
		f.Stage = a.Stage
	}
	if errors.As(e, &own) {
		f.Code = own.Code
		f.Outcome = own.Outcome
	}
	if errors.Is(e, context.Canceled) {
		f.Code = "cancelled"
		f.Outcome = "unknown"
	}
	return f
}
func (r *runner) wait(c *api.Client, id, target string, value map[string]any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	last := ""
	for {
		state, ok := value["state"].(string)
		if !ok {
			return nil, fail("invalid_response")
		}
		if state == target {
			return value, nil
		}
		switch state {
		case "failed", "closed", "cleanup_failed":
			return nil, fail("world_" + state)
		case "creating", "ready", "resetting", "deleting":
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
			return nil, &failure{Code: "world_wait_timeout", Outcome: "unknown"}
		case <-time.After(500 * time.Millisecond):
		}
		var e error
		value, e = c.Request(ctx, "GET", "/v1/worlds/"+id, nil, "")
		if e != nil {
			if ctx.Err() != nil && r.ctx.Err() == nil {
				return nil, &failure{Code: "world_wait_timeout", Outcome: "unknown"}
			}
			return nil, e
		}
	}
}

func decodeObject(reader io.Reader) (map[string]any, error) {
	data, e := io.ReadAll(io.LimitReader(reader, 65537))
	if e != nil || len(data) > 65536 {
		return nil, fail("invalid_json_input")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var result map[string]any
	if decoder.Decode(&result) != nil || result == nil {
		return nil, fail("invalid_json_input")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fail("invalid_json_input")
	}
	return result, nil
}
func (r *runner) readObject(path string) (map[string]any, error) {
	if path == "-" {
		return decodeObject(r.in)
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, fail("input_read_failed")
	}
	defer f.Close()
	return decodeObject(f)
}
func public(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for k, v := range value {
		if k != "agent_token" {
			out[k] = v
		}
	}
	return out
}
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}
func (r *runner) emit(value any) error {
	e := json.NewEncoder(r.out)
	if !r.opts.json {
		e.SetIndent("", "  ")
	}
	return e.Encode(value)
}

func (r *runner) progress(label, value string) {
	if !r.opts.json {
		fmt.Fprintln(r.err, label, value)
	}
}
