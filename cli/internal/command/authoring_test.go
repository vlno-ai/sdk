package command

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

const roleSecret = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"

func roleArgs(path string) []string {
	return []string{"authoring", "role", testID, "--role", "writer", "--provider", "anthropic", "--model", "claude-sonnet-5", "--session", "writer-session", "--token-file", path, "--json"}
}

func TestAuthoringCreateContextAndFreeze(t *testing.T) {
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Error("owner credential missing")
		}
		switch r.URL.Path {
		case "/v1/authoring-jobs":
			var value map[string]any
			json.NewDecoder(r.Body).Decode(&value)
			if r.Method != "POST" || r.Header.Get("Idempotency-Key") != "authoring-replay" || value["task"] != "Archive the selected note." {
				t.Error("bad create request")
			}
			io.WriteString(w, `{"id":"`+testID+`","state":"awaiting_seed"}`)
		case "/v1/authoring-jobs/" + testID + "/context":
			if r.Method != "GET" {
				t.Error("context must GET")
			}
			io.WriteString(w, `{"job":{"id":"`+testID+`"},"app_mapping":{}}`)
		case "/v1/authoring-jobs/" + testID + "/freeze":
			body, _ := io.ReadAll(r.Body)
			if r.Method != "POST" || string(body) != `{"suite":"authored-notes@1"}` {
				t.Error("bad freeze request")
			}
			io.WriteString(w, `{"ref":"authored-notes@1","cases":[]}`)
		default:
			if r.Method != "GET" || r.URL.Path != "/v1/authoring-jobs/"+testID {
				t.Error("unexpected route")
			}
			io.WriteString(w, `{"id":"`+testID+`","state":"awaiting_seed"}`)
		}
	})
	brief := `{"template":"notes-workspace@1","task":"Archive the selected note.","requirements":[{"id":"archive","category":"task","description":"Archive only the selected note."}]}`
	code, out, err := invoke(t, []string{"authoring", "create", "--file", "-", "--idempotency-key", "authoring-replay", "--json"}, brief)
	if code != 0 || err != "" || decode(t, out)["idempotency_key"] != "authoring-replay" {
		t.Fatal(code, out, err)
	}
	for _, action := range []string{"status", "context", "freeze"} {
		args := []string{"authoring", action, testID, "--json"}
		if action == "freeze" {
			args = append(args, "--suite", "authored-notes@1")
		}
		code, out, err := invoke(t, args, "")
		if code != 0 || err != "" {
			t.Fatal(code, out, err)
		}
		decode(t, out)
	}
}

func TestAuthoringRoleTokenFileExclusiveAndPrivate(t *testing.T) {
	var assignments atomic.Int32
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		assignments.Add(1)
		if r.Method != "POST" || r.URL.Path != "/v1/authoring-jobs/"+testID+"/roles" {
			t.Error("wrong role route")
		}
		var value map[string]any
		json.NewDecoder(r.Body).Decode(&value)
		if value["role"] != "writer" || value["session_id"] != "writer-session" {
			t.Error(value)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": testID, "role": "writer", "provider": "anthropic", "model": "claude-sonnet-5", "session_id": "writer-session", "token": roleSecret})
	})
	path := filepath.Join(t.TempDir(), "writer.token")
	code, out, err := invoke(t, roleArgs(path), "")
	if code != 0 || err != "" || strings.Contains(out+err, roleSecret) || decode(t, out)["token"] != nil {
		t.Fatal(code, out, err)
	}
	data, e := os.ReadFile(path)
	if e != nil || string(data) != roleSecret+"\n" {
		t.Fatal("credential not stored correctly")
	}
	info, e := os.Stat(path)
	if e != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatal("credential file is not private")
	}
	code, _, err = invoke(t, roleArgs(path), "")
	if code != 1 || assignments.Load() != 1 || !strings.Contains(err, "token_file_create_failed") {
		t.Fatal(code, assignments.Load(), err)
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(t.TempDir(), "existing-link")
		if e := os.Symlink(path, link); e != nil {
			t.Fatal(e)
		}
		code, _, _ = invoke(t, roleArgs(link), "")
		if code != 1 || assignments.Load() != 1 {
			t.Fatal("followed existing symlink")
		}
	}
	unchanged, _ := os.ReadFile(path)
	if string(unchanged) != string(data) {
		t.Fatal("overwrote credential file")
	}
}

func TestAuthoringRoleFailureRemovesReservation(t *testing.T) {
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"code":"unauthorized"}}`)
	})
	path := filepath.Join(t.TempDir(), "unissued.token")
	code, out, err := invoke(t, roleArgs(path), "")
	if code != 1 || out != "" || decode(t, err)["error"].(map[string]any)["job_id"] != testID {
		t.Fatal(code, out, err)
	}
	if _, e := os.Lstat(path); !os.IsNotExist(e) {
		t.Fatal("failed assignment left a token file")
	}
}

func TestAuthoringSubmitUsesRoleCredential(t *testing.T) {
	basis := strings.Repeat("c", 64)
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+roleSecret {
			t.Error("wrong submission credential")
		}
		if r.Method != "POST" || r.URL.Path != "/v1/authoring-jobs/"+testID+"/artifacts" {
			t.Error("bad artifact route")
		}
		var value map[string]any
		json.NewDecoder(r.Body).Decode(&value)
		if value["basis_sha256"] != basis || value["artifact"].(map[string]any)["identities"] == nil {
			t.Error(value)
		}
		io.WriteString(w, `{"id":"`+testID+`","state":"awaiting_tests"}`)
	})
	t.Setenv("VLNO_API_KEY", roleSecret)
	code, out, err := invoke(t, []string{"authoring", "submit", testID, "--file", "-", "--basis", basis, "--json"}, `{"identities":{},"fixtures":[],"success_state":[]}`)
	if code != 0 || err != "" || decode(t, out)["state"] != "awaiting_tests" || strings.Contains(out, roleSecret) {
		t.Fatal(code, out, err)
	}
}

func TestAuthoringRunIncludesMandatoryLiveGate(t *testing.T) {
	var posts, polls atomic.Int32
	states := []string{"ready_for_live", "live_evaluating", "accepted"}
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		state := "awaiting_seed"
		if r.Method == "POST" {
			posts.Add(1)
			if r.URL.Path != "/v1/authoring-jobs/"+testID+"/run" {
				t.Error("wrong run route")
			}
		} else {
			state = states[polls.Add(1)-1]
		}
		json.NewEncoder(w).Encode(map[string]any{"id": testID, "state": state, "runner": map[string]any{"state": "running"}})
	})
	code, out, err := invoke(t, []string{"authoring", "run", testID, "--json"}, "")
	if code != 0 || err != "" || decode(t, out)["state"] != "accepted" || posts.Load() != 1 || polls.Load() != 3 {
		t.Fatal(code, out, err)
	}
}

func TestAuthoringValidationStopsBeforeAuditAndLiveWaits(t *testing.T) {
	var action string
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		state := "awaiting_audit"
		if action == "live" {
			state = "accepted"
		}
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/"+action) {
			t.Error("wrong action")
		}
		json.NewEncoder(w).Encode(map[string]any{"id": testID, "state": state})
	})
	for _, current := range []string{"validate", "live"} {
		action = current
		code, out, err := invoke(t, []string{"authoring", current, testID, "--json"}, "")
		if code != 0 || err != "" {
			t.Fatal(code, out, err)
		}
	}
}

func TestAuthoringFailuresAndTimeoutKeepJobID(t *testing.T) {
	for _, state := range []string{"live_failed", "rejected", "validation_failed", "validating", "runner_failed"} {
		t.Run(state, func(t *testing.T) {
			worker(t, func(w http.ResponseWriter, r *http.Request) {
				value := map[string]any{"id": testID, "state": state}
				if state == "runner_failed" {
					value["state"] = "awaiting_seed"
					value["runner"] = map[string]any{"state": "failed", "error": "private provider details"}
				}
				json.NewEncoder(w).Encode(value)
			})
			code, out, err := invoke(t, []string{"authoring", "run", testID, "--wait-timeout", "2ms", "--json"}, "")
			if code != 1 || out != "" || decode(t, err)["error"].(map[string]any)["job_id"] != testID || strings.Contains(err, "private provider") {
				t.Fatal(code, out, err)
			}
		})
	}
}
