package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const agentToken = "cccccccccccccccccccccccccccccccc"
const suiteWorld = "dddddddddddddddddddddddddddddddd"

// A separate native process exercises the actual stdin/environment boundary.
func TestSuiteAgentProcess(t *testing.T) {
	mode := os.Getenv("TEST_SUITE_AGENT")
	if mode == "" {
		return
	}
	if mode == "sleep" {
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	var input map[string]any
	if json.NewDecoder(os.Stdin).Decode(&input) != nil {
		os.Exit(21)
	}
	if os.Getenv("VLNO_API_KEY") != "" || os.Getenv("VLNO_CONFIG") != "" || os.Getenv("UNLISTED_PROVIDER_KEY") != "" || os.Getenv("OPT_PROVIDER_KEY") != "allowed" || len(input) != 4 {
		os.Exit(22)
	}
	if mode == "error" {
		fmt.Fprintln(os.Stderr, agentToken)
		os.Exit(1)
	}
	app := input["app"].(map[string]any)
	req, _ := http.NewRequest("POST", app["endpoint"].(string)+"/v1/worlds/"+app["world_id"].(string)+"/call", strings.NewReader(`{"operation":"archive_note","arguments":{"note_id":1}}`))
	req.Header.Set("Authorization", "Bearer "+app["token"].(string))
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		os.Exit(23)
	}
	resp.Body.Close()
	// Large output is consumed with constant memory and never returned to the user.
	for i := 0; i < 256; i++ {
		fmt.Fprint(os.Stdout, strings.Repeat(agentToken, 1024))
		fmt.Fprint(os.Stderr, agentToken)
	}
	os.Exit(0)
}
func suiteAgentFile(t *testing.T, mode string) string {
	t.Helper()
	t.Setenv("TEST_SUITE_AGENT", mode)
	t.Setenv("OPT_PROVIDER_KEY", "allowed")
	t.Setenv("UNLISTED_PROVIDER_KEY", "not_allowed")
	f := filepath.Join(t.TempDir(), "agent.json")
	data, _ := json.Marshal(agentConfig{Argv: []string{os.Args[0], "-test.run=^TestSuiteAgentProcess$"}, EnvKeys: []string{"TEST_SUITE_AGENT", "OPT_PROVIDER_KEY"}})
	if err := os.WriteFile(f, data, 0600); err != nil {
		t.Fatal(err)
	}
	return f
}
func TestSuiteRunLocalAgentAndPrivateCapability(t *testing.T) {
	for _, mode := range []string{"success", "error", "sleep"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			finished, called, cancelled := false, false, false
			claim := ""
			finishStatus := ""
			run := func() map[string]any {
				state, caseState := "running", "running"
				if finished {
					state = "completed"
					caseState = "passed"
					if mode != "success" {
						caseState = "error"
					}
				}
				return map[string]any{"id": testID, "state": state, "cases": []any{map[string]any{"index": 0, "ref": "sample@1", "state": caseState, "cleanup_confirmed": finished}}}
			}
			worker(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				var p map[string]any
				if r.Method == "POST" {
					json.NewDecoder(r.Body).Decode(&p)
				}
				if strings.HasSuffix(r.URL.Path, "/task") || strings.HasSuffix(r.URL.Path, "/operations") || strings.HasSuffix(r.URL.Path, "/call") {
					if r.Header.Get("Authorization") != "Bearer "+agentToken {
						t.Error("wrong agent capability")
					}
				} else if r.Header.Get("Authorization") != "Bearer "+testKey {
					t.Error("owner missing")
				}
				switch r.URL.Path {
				case "/v1/suite-runs":
					if p["revision"] != "candidate-v1" || p["suite"] != "sample@1" || r.Header.Get("Idempotency-Key") != "stable-run" {
						t.Error("bad create")
					}
					value, present := p["environment"]
					if present != (mode == "success") || (present && value != true) {
						t.Error("environment opt-in was not preserved")
					}
					json.NewEncoder(w).Encode(run())
				case "/v1/suite-runs/" + testID:
					json.NewEncoder(w).Encode(run())
				case "/v1/suite-runs/" + testID + "/next":
					claim, _ = p["claim"].(string)
					if !roleToken.MatchString(claim) {
						t.Error("bad claim")
					}
					json.NewEncoder(w).Encode(map[string]any{"run": run(), "case": map[string]any{"index": 0, "ref": "sample@1", "world": map[string]any{"id": suiteWorld, "state": "creating", "agent_token": agentToken}}})
				case "/v1/worlds/" + suiteWorld:
					json.NewEncoder(w).Encode(map[string]any{"id": suiteWorld, "state": "ready", "agent_token": agentToken})
				case "/v1/worlds/" + suiteWorld + "/task":
					io.WriteString(w, `{"task":"Archive note 1","hidden_extra":"must not pass to child"}`)
				case "/v1/worlds/" + suiteWorld + "/operations":
					io.WriteString(w, `{"operations":{"archive_note":{}}}`)
				case "/v1/worlds/" + suiteWorld + "/call":
					called = true
					io.WriteString(w, `{}`)
				case "/v1/suite-runs/" + testID + "/finish":
					if p["claim"] != claim || p["case_index"] != float64(0) {
						t.Error("wrong finish")
					}
					finishStatus, _ = p["agent_status"].(string)
					finished = true
					json.NewEncoder(w).Encode(run())
				case "/v1/suite-runs/" + testID + "/cancel":
					cancelled = true
					json.NewEncoder(w).Encode(run())
				default:
					t.Error("unexpected route", r.URL.Path)
					http.Error(w, "bad", 500)
				}
			})
			file := suiteAgentFile(t, mode)
			timeout := "2s"
			if mode == "sleep" {
				timeout = "40ms"
			}
			args := []string{"suites", "run", "sample@1", "--revision", "candidate-v1", "--agent-config", file, "--idempotency-key", "stable-run", "--case-timeout", timeout, "--json"}
			if mode == "success" {
				args = append(args, "--environment")
			}
			code, out, stderr := invoke(t, args, "")
			mu.Lock()
			defer mu.Unlock()
			wantCode := 1
			wantStatus := mode
			if mode == "success" {
				wantCode = 0
				wantStatus = "completed"
			}
			if mode == "sleep" {
				wantStatus = "timeout"
			}
			if code != wantCode || stderr != "" || !finished || cancelled || finishStatus != wantStatus || called != (mode == "success") || strings.Contains(out+stderr, agentToken) || strings.Contains(out+stderr, claim) || strings.Contains(out, testKey) {
				t.Fatal(code, out, stderr, finishStatus, called, cancelled)
			}
		})
	}
}
func TestSuiteConfigRejectsInvalidBeforeRequests(t *testing.T) {
	isolated(t)
	for _, raw := range []string{`null`, `{"argv":[]}`, `{"argv":["echo"],"input_format":"unknown"}`, `{"argv":["echo"],"unknown":true}`, `{"argv":["echo"],"argv":["other"]}`, `{"argv":["echo"],"env_keys":["VLNO_API_KEY"]}`, `{"argv":["echo"],"env_keys":["WORKER_KEY"]}`, `{"argv":["echo"],"env_keys":["RUN_CLAIM"]}`, `{"argv":["echo"]} {}`} {
		f := filepath.Join(t.TempDir(), "agent.json")
		os.WriteFile(f, []byte(raw), 0600)
		code, _, err := invoke(t, []string{"suites", "run", "sample@1", "--revision", "v1", "--agent-config", f, "--json"}, "")
		if code != 2 || !strings.Contains(err, "invalid_agent_config") {
			t.Fatal(code, err, raw)
		}
	}
}
func TestAgentEnvironmentDropsSecretAliases(t *testing.T) {
	t.Setenv("VLNO_API_KEY", testKey)
	t.Setenv("PROVIDER_KEY", "legitimate")
	t.Setenv("OWNER_ALIAS", "prefix"+testKey+"suffix")
	t.Setenv("UNLISTED_KEY", "ambient")
	env := strings.Join(agentEnv(agentConfig{EnvKeys: []string{"PROVIDER_KEY", "OWNER_ALIAS", "VLNO_API_KEY"}}, testKey), "\n")
	if !strings.Contains(env, "PROVIDER_KEY=legitimate") || strings.Contains(env, testKey) || strings.Contains(env, "UNLISTED_KEY") {
		t.Fatal("environment secret boundary failed")
	}
}
func TestSuiteCancelledContextKillsAgent(t *testing.T) {
	file := suiteAgentFile(t, "sleep")
	cfg, err := loadAgentConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)
	start := time.Now()
	status := executeAgent(ctx, cfg, map[string]any{}, agentEnv(cfg))
	if status != "timeout" || time.Since(start) > time.Second {
		t.Fatal(status, time.Since(start))
	}
}
func TestRunCompareExitStatusAndRoutes(t *testing.T) {
	for _, kind := range []string{"unchanged", "regression", "incomparable"} {
		t.Run(kind, func(t *testing.T) {
			worker(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/suite-run-comparisons" || r.Method != "POST" {
					t.Error("wrong route")
				}
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				if body["baseline"] != testID || body["candidate"] != suiteWorld {
					t.Error("wrong pair")
				}
				counts := map[string]int{"regression": 0, "incomparable": 0, "unchanged": 0}
				counts[kind] = 1
				json.NewEncoder(w).Encode(map[string]any{"counts": counts})
			})
			code, out, err := invoke(t, []string{"runs", "compare", testID, suiteWorld, "--json"}, "")
			want := 1
			if kind == "unchanged" {
				want = 0
			}
			if code != want || out == "" || err != "" {
				t.Fatal(code, out, err)
			}
		})
	}
}
func TestSuiteRunDeadlineCancelsWithRunID(t *testing.T) {
	file := suiteAgentFile(t, "sleep")
	cancelled := false
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/suite-runs":
			io.WriteString(w, `{"id":"`+testID+`","state":"pending"}`)
		case "/v1/suite-runs/" + testID + "/next":
			io.WriteString(w, `{"run":{"id":"`+testID+`","state":"running"},"case":{"index":0,"ref":"sample@1","world":{"id":"`+suiteWorld+`","state":"creating"}}}`)
		case "/v1/suite-runs/" + testID + "/cancel":
			cancelled = true
			io.WriteString(w, `{}`)
		default:
			t.Error("unexpected request")
		}
	})
	code, out, err := invoke(t, []string{"suites", "run", "sample@1", "--revision", "v1", "--agent-config", file, "--wait-timeout", "30ms", "--json"}, "")
	if code != 1 || out != "" || !strings.Contains(err, testID) || !cancelled {
		t.Fatal(code, out, err, cancelled)
	}
}
func TestAgentOutputNeverLeaksOnStartFailure(t *testing.T) {
	var out bytes.Buffer
	status := executeAgent(context.Background(), agentConfig{Argv: []string{"/nonexistent/" + agentToken}}, map[string]any{}, nil)
	if status != "error" || out.Len() != 0 {
		t.Fatal(status)
	}
}

func TestWorkerCancellationStopsLocalAgent(t *testing.T) {
	file := suiteAgentFile(t, "sleep")
	cfg, err := loadAgentConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/suite-runs/"+testID {
			t.Error("wrong monitor request")
		}
		io.WriteString(w, `{"id":"`+testID+`","state":"cancelled"}`)
	})
	r := runner{ctx: context.Background(), opts: options{timeout: time.Second}}
	c, err := r.client()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	status, err := executeSuiteAgent(context.Background(), 10*time.Second, cfg, map[string]any{}, agentEnv(cfg), c, testID)
	if status != "" || err == nil || err.Error() != "suite_run_stopped" || time.Since(start) > 3*time.Second {
		t.Fatal(status, err, time.Since(start))
	}
}
func TestReplayedRunClaimConflictDoesNotCancelOtherInvocation(t *testing.T) {
	file := suiteAgentFile(t, "sleep")
	calls := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/v1/suite-runs" {
			io.WriteString(w, `{"id":"`+testID+`","state":"running"}`)
			return
		}
		if r.URL.Path != "/v1/suite-runs/"+testID+"/next" {
			t.Error("claim conflict must not cancel another driver")
		}
		w.WriteHeader(409)
		io.WriteString(w, `{"error":{"code":"case_claimed"}}`)
	})
	code, out, err := invoke(t, []string{"suites", "run", "sample@1", "--revision", "v1", "--agent-config", file, "--json"}, "")
	if code != 1 || out != "" || calls != 2 || !strings.Contains(err, "case_claimed") || !strings.Contains(err, testID) {
		t.Fatal(code, out, err, calls)
	}
}
