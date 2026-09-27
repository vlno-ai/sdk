package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testKey = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func invoke(t *testing.T, args []string, input string) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	code := Run(context.Background(), args, strings.NewReader(input), &out, &err)
	return code, out.String(), err.String()
}
func isolated(t *testing.T) {
	t.Helper()
	t.Setenv("VLNO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	for _, key := range []string{"VLNO_ENDPOINT", "VLNO_API_KEY", "VLNO_CA_FILE"} {
		t.Setenv(key, "")
	}
}
func worker(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	isolated(t)
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	t.Setenv("VLNO_ENDPOINT", s.URL)
	t.Setenv("VLNO_API_KEY", testKey)
	return s
}
func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if e := json.Unmarshal([]byte(s), &v); e != nil {
		t.Fatalf("not one JSON object: %q (%v)", s, e)
	}
	return v
}
func TestHelpAndUnavailableFeaturesNeedNoCredentials(t *testing.T) {
	isolated(t)
	for _, args := range [][]string{{"--help"}, {"worlds", "create", "--help"}, {"version", "--json"}} {
		code, out, err := invoke(t, args, "")
		if code != 0 || out == "" || err != "" {
			t.Fatalf("%v: %d %s %s", args, code, out, err)
		}
	}
	code, out, err := invoke(t, []string{"worlds", "connect", testID, "--json"}, "")
	if code != 2 || out != "" || decode(t, err)["error"].(map[string]any)["code"] != "usage" {
		t.Fatal(code, out, err)
	}
}
func TestCatalogLoginLogout(t *testing.T) {
	calls := 0
	s := worker(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+testKey || r.URL.Path != "/v1/catalog" {
			t.Error("incorrect request")
		}
		io.WriteString(w, `{"apps":{"notes":{"app":{"id":"tiny_notes"},"image_id":"sha256:123"}}}`)
	})
	t.Setenv("VLNO_API_KEY", "")
	code, out, err := invoke(t, []string{"login", "--key-stdin", "--json"}, testKey+"\n")
	if code != 0 || decode(t, out)["endpoint"] != s.URL || err != "" {
		t.Fatal(code, out, err)
	}
	if strings.Contains(out+err, testKey) {
		t.Fatal("credential exposed")
	}
	t.Setenv("VLNO_ENDPOINT", "")
	code, out, err = invoke(t, []string{"--json", "catalog", "apps", "show", "notes"}, "")
	if code != 0 || decode(t, out)["app"].(map[string]any)["id"] != "tiny_notes" || err != "" {
		t.Fatal(code, out, err)
	}
	code, out, err = invoke(t, []string{"logout", "--json"}, "")
	if code != 0 || decode(t, out)["logged_out"] != true {
		t.Fatal(code, out, err)
	}
	if _, e := os.Stat(os.Getenv("VLNO_CONFIG")); !os.IsNotExist(e) {
		t.Fatal("config not removed")
	}
	code, out, err = invoke(t, []string{"catalog", "apps", "--json"}, "")
	if code == 0 || decode(t, err)["error"].(map[string]any)["code"] != "missing_credentials" || calls != 2 {
		t.Fatal(code, out, err, calls)
	}
}
func TestCreateWaitAndReplayMetadata(t *testing.T) {
	posts, gets := 0, 0
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			posts++
			if r.Header.Get("Idempotency-Key") != "stable-replay" {
				t.Error("key missing")
			}
			var p map[string]any
			json.NewDecoder(r.Body).Decode(&p)
			if p["image"] != "notes" || p["ttl_seconds"] != float64(900) {
				t.Error("wrong payload", p)
			}
			io.WriteString(w, `{"id":"`+testID+`","state":"creating","agent_token":"hidden"}`)
		case "GET":
			gets++
			io.WriteString(w, `{"id":"`+testID+`","state":"ready","agent_token":"hidden"}`)
		}
	})
	code, out, err := invoke(t, []string{"worlds", "create", "--app", "notes", "--idempotency-key", "stable-replay", "--json"}, "")
	v := decode(t, out)
	if code != 0 || err != "" || v["state"] != "ready" || v["idempotency_key"] != "stable-replay" || posts != 1 || gets != 1 || strings.Contains(out, "agent_token") {
		t.Fatal(code, out, err, posts, gets)
	}
}
func TestWaitTimeoutHasRecoveryMetadataAndOneJSONError(t *testing.T) {
	posts := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
		}
		io.WriteString(w, `{"id":"`+testID+`","state":"creating","agent_token":"hidden"}`)
	})
	code, out, err := invoke(t, []string{"worlds", "create", "--app", "notes", "--json", "--wait-timeout", "2ms"}, "")
	f := decode(t, err)["error"].(map[string]any)
	if code == 0 || out != "" || f["code"] != "world_wait_timeout" || f["world_id"] != testID || f["idempotency_key"] == "" || f["outcome"] != "unknown" || posts != 1 {
		t.Fatal(code, out, err, posts)
	}
}
func TestNoWaitAndResetCloseRoutes(t *testing.T) {
	var routes []string
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		routes = append(routes, r.Method+" "+r.URL.Path)
		io.WriteString(w, `{"id":"`+testID+`","state":"resetting","agent_token":"hidden"}`)
	})
	for _, action := range []string{"reset", "destroy"} {
		code, out, err := invoke(t, []string{"worlds", action, testID, "--no-wait", "--json"}, "")
		if code != 0 || err != "" || strings.Contains(out, "agent_token") {
			t.Fatal(code, out, err)
		}
	}
	if len(routes) != 2 || routes[0] != "POST /v1/worlds/"+testID+"/reset" || routes[1] != "DELETE /v1/worlds/"+testID {
		t.Fatal(routes)
	}
}
func TestJSONArgsPreserveLargeIDsAndRoutePrivileges(t *testing.T) {
	var bodies []string
	var routes []string
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		routes = append(routes, r.URL.Path)
		io.WriteString(w, `{"status":200,"body":{"id":9007199254740993}}`)
	})
	for _, action := range []string{"seed", "call"} {
		code, out, err := invoke(t, []string{"worlds", action, testID, "get_note", "--args-file", "-", "--json"}, `{"note_id":9007199254740993}`)
		if code != 0 || err != "" || !strings.Contains(out, "9007199254740993") {
			t.Fatal(code, out, err)
		}
	}
	if !strings.Contains(bodies[0], "9007199254740993") || !strings.Contains(bodies[0], `"alias":"primary"`) || strings.Contains(bodies[1], `"alias"`) {
		t.Fatal(bodies)
	}
	if !strings.HasSuffix(routes[0], "/fixtures") || !strings.HasSuffix(routes[1], "/call") {
		t.Fatal(routes)
	}
}
func TestInvalidInputDoesNotSend(t *testing.T) {
	calls := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	cases := [][]string{
		{"worlds", "status", "../bad"},
		{"worlds", "create", "--app", "notes", "--ttl", "1"},
		{"worlds", "create", "--app", "notes", "--idempotency-key", "short"},
		{"worlds", "call", testID, "profile", "--args", "[]"},
		{"worlds", "call", testID, "profile", "--args", "{} {}"},
		{"worlds", "call", testID, "profile", "--args", `{"x":1}`, "--args-file", "-"},
	}
	for _, args := range cases {
		code, out, err := invoke(t, append(args, "--json"), "")
		if code == 0 || out != "" {
			t.Fatal(code, out, err)
		}
		decode(t, err)
	}
	if calls != 0 {
		t.Fatal("invalid input reached server")
	}
}
func TestCancellationIncludesWorldAndKey(t *testing.T) {
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"`+testID+`","state":"resetting"}`)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, err bytes.Buffer
	code := Run(ctx, []string{"worlds", "reset", testID, "--json", "--idempotency-key", "cancel-key"}, strings.NewReader(""), &out, &err)
	f := decode(t, err.String())["error"].(map[string]any)
	if code != 130 || f["world_id"] != testID || f["idempotency_key"] != "cancel-key" {
		t.Fatal(code, out.String(), err.String())
	}
}

func TestVersionedCatalogAndFilters(t *testing.T) {
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/catalog" {
			t.Error("incorrect catalog request")
		}
		io.WriteString(w, `{"apps":{},"worlds":{"notes-workspace@1":{"ref":"notes-workspace@1","app":"notes","description":"Notes","scenarios":["archive-note@1"]},"tickets-workspace@1":{"ref":"tickets-workspace@1","app":"tickets","description":"Tickets","scenarios":[]}},"scenarios":{"archive-note@1":{"ref":"archive-note@1","template":"notes-workspace@1","description":"Archive","task":"Archive the selected note."},"resolve-ticket@1":{"ref":"resolve-ticket@1","template":"tickets-workspace@1","description":"Resolve","task":"Resolve the selected ticket."}}}`)
	})
	for _, tc := range []struct{ kind, flag, filter, key string }{
		{"worlds", "--app", "notes", "notes-workspace@1"},
		{"scenarios", "--world", "notes-workspace@1", "archive-note@1"},
	} {
		code, out, err := invoke(t, []string{"catalog", tc.kind, tc.flag, tc.filter, "--json"}, "")
		if code != 0 || err != "" {
			t.Fatal(code, out, err)
		}
		entries := decode(t, out)[tc.kind].(map[string]any)
		if len(entries) != 1 || entries[tc.key] == nil {
			t.Fatal(entries)
		}
		code, out, err = invoke(t, []string{"catalog", tc.kind, "show", tc.key, "--json"}, "")
		if code != 0 || err != "" || decode(t, out)["ref"] != tc.key {
			t.Fatal(code, out, err)
		}
		code, out, err = invoke(t, []string{"catalog", tc.kind, tc.flag, "missing", "--json"}, "")
		if code != 0 || err != "" || len(decode(t, out)[tc.kind].(map[string]any)) != 0 {
			t.Fatal(code, out, err)
		}
	}
	for kind, missing := range map[string]string{"worlds": "world_template_not_found", "scenarios": "scenario_not_found"} {
		code, out, err := invoke(t, []string{"catalog", kind, "show", "missing@1", "--json"}, "")
		if code != 1 || out != "" || decode(t, err)["error"].(map[string]any)["code"] != missing {
			t.Fatal(code, out, err)
		}
		code, out, err = invoke(t, []string{"catalog", kind}, "")
		if code != 0 || err != "" || !strings.Contains(out, "notes-workspace@1") {
			t.Fatal(code, out, err)
		}
	}
}

func TestTemplateCreationAndTask(t *testing.T) {
	posts := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v1/worlds" {
			posts++
			var payload map[string]any
			if json.NewDecoder(r.Body).Decode(&payload) != nil {
				t.Error("invalid payload")
			}
			if payload["template"] != "notes-workspace@1" || payload["fixture"] != "archive-note@1" || payload["image"] != nil || payload["ttl_seconds"] != float64(900) {
				t.Error(payload)
			}
			io.WriteString(w, `{"id":"`+testID+`","state":"ready","template":"notes-workspace@1","fixture":"archive-note@1","task":"Archive the selected note.","agent_token":"hidden"}`)
			return
		}
		if r.Method != "GET" || r.URL.Path != "/v1/worlds/"+testID+"/task" {
			t.Error("wrong task route")
		}
		io.WriteString(w, `{"task":"Archive the selected note.","template":"notes-workspace@1","fixture":"archive-note@1","generation":1}`)
	})
	code, out, err := invoke(t, []string{"worlds", "create", "--template", "notes-workspace@1", "--fixture", "archive-note@1", "--json"}, "")
	if code != 0 || err != "" || decode(t, out)["fixture"] != "archive-note@1" || strings.Contains(out, "agent_token") || posts != 1 {
		t.Fatal(code, out, err)
	}
	code, out, err = invoke(t, []string{"worlds", "task", testID, "--json"}, "")
	if code != 0 || err != "" || decode(t, out)["task"] != "Archive the selected note." {
		t.Fatal(code, out, err)
	}
}

func TestInvalidTemplateSelectionDoesNotSend(t *testing.T) {
	calls := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for _, args := range [][]string{
		{"--app", "notes", "--template", "notes-workspace@1"},
		{"--app", "notes", "--fixture", "archive-note@1"},
		{"--fixture", "archive-note@1"},
		{"--template", "notes-workspace"},
		{"--template", "notes-workspace@0"},
		{"--template", "notes-workspace@1", "--fixture", "archive-note@01"},
		{"--template", strings.Repeat("n", 49) + "@1"},
	} {
		code, out, err := invoke(t, append(append([]string{"worlds", "create"}, args...), "--json"), "")
		if code != 2 || out != "" {
			t.Fatal(code, out, err)
		}
		decode(t, err)
	}
	if calls != 0 {
		t.Fatal("invalid selection reached worker")
	}
}

func TestEvaluatePrintsReportAndSetsExitStatus(t *testing.T) {
	for _, status := range []string{"passed", "failed", "inconclusive", "invalid"} {
		t.Run(status, func(t *testing.T) {
			worker(t, func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.URL.Path != "/v1/worlds/"+testID+"/evaluate" || string(body) != "{}" || r.Header.Get("Idempotency-Key") != "" {
					t.Error("wrong evaluation request")
				}
				json.NewEncoder(w).Encode(map[string]any{"status": status, "world_id": testID, "task_completed": status == "passed", "policy_violations": []any{}, "assertions": []any{}, "evidence_complete": status == "passed", "limitations": []any{}})
			})
			for _, jsonMode := range []bool{true, false} {
				args := []string{"worlds", "evaluate", testID}
				if jsonMode {
					args = append(args, "--json")
				}
				code, out, err := invoke(t, args, "")
				want := 1
				if status == "passed" {
					want = 0
				}
				if code != want || err != "" || decode(t, out)["status"] != status {
					t.Fatal(code, out, err)
				}
			}
		})
	}
}
