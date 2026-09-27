package command

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func generationArgs(extra ...string) []string {
	return append([]string{"scenarios", "generate", "--family", "archive-note", "--template", "notes-workspace@1", "--seed", "7", "--idempotency-key", "generation-replay", "--json"}, extra...)
}

func TestScenarioJobGenerationPollingAndFreeze(t *testing.T) {
	posts, polls, freezes := 0, 0, 0
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Error("missing auth")
		}
		switch r.URL.Path {
		case "/v1/scenario-jobs":
			posts++
			var payload map[string]any
			json.NewDecoder(r.Body).Decode(&payload)
			if r.Method != "POST" || r.Header.Get("Idempotency-Key") != "generation-replay" || payload["family"] != "archive-note" || payload["template"] != "notes-workspace@1" || payload["seed"] != float64(7) || payload["count"] != float64(2) || payload["distractors"] != float64(2) {
				t.Error(payload)
			}
			io.WriteString(w, `{"id":"`+testID+`","state":"queued","cases":[]}`)
		case "/v1/scenario-jobs/" + testID:
			polls++
			if r.Method != "GET" {
				t.Error("status must GET")
			}
			io.WriteString(w, `{"id":"`+testID+`","state":"validated","cases":[{"ref":"generated-note@1"}]}`)
		case "/v1/scenario-jobs/" + testID + "/freeze":
			freezes++
			body, _ := io.ReadAll(r.Body)
			if r.Method != "POST" || string(body) != `{"suite":"notes-regression@1"}` || r.Header.Get("Idempotency-Key") != "" {
				t.Error("wrong freeze request")
			}
			io.WriteString(w, `{"ref":"notes-regression@1","job_id":"`+testID+`","cases":[],"suite_sha256":"hash"}`)
		default:
			t.Error("unexpected route")
		}
	})
	code, out, err := invoke(t, generationArgs(), "")
	if code != 0 || err != "" || decode(t, out)["state"] != "validated" || decode(t, out)["idempotency_key"] != "generation-replay" || posts != 1 || polls != 1 {
		t.Fatal(code, out, err)
	}
	code, out, err = invoke(t, []string{"scenarios", "status", testID, "--json"}, "")
	if code != 0 || err != "" || decode(t, out)["id"] != testID || polls != 2 {
		t.Fatal(code, out, err)
	}
	code, out, err = invoke(t, []string{"scenarios", "freeze", testID, "--suite", "notes-regression@1", "--json"}, "")
	if code != 0 || err != "" || decode(t, out)["ref"] != "notes-regression@1" || freezes != 1 {
		t.Fatal(code, out, err)
	}
}

func TestScenarioTimeoutAndFailedJobPreserveReceipt(t *testing.T) {
	for _, state := range []string{"running", "failed"} {
		t.Run(state, func(t *testing.T) {
			posts := 0
			worker(t, func(w http.ResponseWriter, r *http.Request) {
				posts++
				io.WriteString(w, `{"id":"`+testID+`","state":"`+state+`","error":"private arbitrary text"}`)
			})
			code, out, err := invoke(t, generationArgs("--wait-timeout", "2ms"), "")
			failure := decode(t, err)["error"].(map[string]any)
			want := "scenario_wait_timeout"
			if state == "failed" {
				want = "scenario_job_failed"
			}
			if code != 1 || out != "" || failure["code"] != want || failure["job_id"] != testID || failure["idempotency_key"] != "generation-replay" || posts != 1 || strings.Contains(err, "private arbitrary") {
				t.Fatal(code, out, err)
			}
		})
	}
}

func TestScenarioNoWaitAndLostResponse(t *testing.T) {
	var calls atomic.Int32
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			io.WriteString(w, `{"id":"`+testID+`","state":"queued"}`)
			return
		}
		io.Copy(io.Discard, r.Body)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	})
	code, out, err := invoke(t, generationArgs("--no-wait"), "")
	if code != 0 || err != "" || decode(t, out)["state"] != "queued" || calls.Load() != 1 {
		t.Fatal(code, out, err)
	}
	code, out, err = invoke(t, generationArgs(), "")
	failure := decode(t, err)["error"].(map[string]any)
	if code != 1 || out != "" || calls.Load() != 2 || failure["code"] != "transport_error" || failure["idempotency_key"] != "generation-replay" || failure["outcome"] != "unknown" {
		t.Fatal(code, out, err)
	}
}

func TestGenerationBoundsFailBeforeNetwork(t *testing.T) {
	calls := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for _, tail := range [][]string{{"--seed", "-1"}, {"--seed", "2147483648"}, {"--count", "0"}, {"--distractors", "5"}, {"--family", "model-prompt"}, {"--template", "notes-workspace"}, {"--idempotency-key", "short"}} {
		code, out, err := invoke(t, generationArgs(tail...), "")
		if code != 2 || out != "" {
			t.Fatal(code, out, err)
		}
	}
	for _, args := range [][]string{{"scenarios", "status", "../outside"}, {"scenarios", "freeze", testID, "--suite", "unversioned"}} {
		code, _, _ := invoke(t, args, "")
		if code != 2 {
			t.Fatal(code)
		}
	}
	if calls != 0 {
		t.Fatal("invalid input reached worker")
	}
}

func TestGenerationCatalogsAndAuthenticationFailure(t *testing.T) {
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/catalog" {
			io.WriteString(w, `{"families":{"archive-note":{"description":"Ordinary archive workflow","compiler_version":1}},"suites":{"notes-regression@1":{"ref":"notes-regression@1","job_id":"`+testID+`","suite_sha256":"hash"}}}`)
			return
		}
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"code":"unauthorized"}}`)
	})
	for _, kind := range []string{"families", "suites"} {
		code, out, err := invoke(t, []string{"catalog", kind, "--json"}, "")
		if code != 0 || err != "" || len(decode(t, out)[kind].(map[string]any)) != 1 {
			t.Fatal(code, out, err)
		}
		code, out, err = invoke(t, []string{"catalog", kind}, "")
		detail := testID
		if kind == "families" {
			detail = "Ordinary archive workflow"
		}
		if code != 0 || err != "" || !strings.Contains(out, detail) || strings.Contains(out, "<nil>") {
			t.Fatal(code, out, err)
		}
		code, out, err = invoke(t, []string{"catalog", kind, "show", "missing", "--json"}, "")
		want := "suite_not_found"
		if kind == "families" {
			want = "scenario_family_not_found"
		}
		if code != 1 || out != "" || decode(t, err)["error"].(map[string]any)["code"] != want {
			t.Fatal(code, out, err)
		}
	}
	code, out, err := invoke(t, generationArgs(), "")
	failure := decode(t, err)["error"].(map[string]any)
	if code != 1 || out != "" || failure["code"] != "unauthorized" || failure["idempotency_key"] != "generation-replay" {
		t.Fatal(code, out, err)
	}
}
