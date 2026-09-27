package command

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func appArgs(action string, extra ...string) []string {
	args := []string{"apps", action, "--file", "-", "--json"}
	if action == "import" {
		args = append(args, "--idempotency-key", "app-import-replay")
	}
	return append(args, extra...)
}

const appManifest = `{"name":"notes","template":"notes@1","description":"Notes workflow","source_image":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","mapping":{},"contract":{}}`

func TestAppsImportStatesValidationAndStatus(t *testing.T) {
	calls, imports := 0, 0
	states := []string{"queued", "importing", "validating", "ready", "ready"}
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Error("missing owner auth")
		}
		if r.Method == "POST" {
			var value map[string]any
			json.NewDecoder(r.Body).Decode(&value)
			if value["name"] != "notes" || value["source_image"] != "sha256:"+strings.Repeat("a", 64) {
				t.Error(value)
			}
		}
		if r.URL.Path == "/v1/app-contracts/validate" {
			if r.Header.Get("Idempotency-Key") != "" {
				t.Error("validation replay key")
			}
			io.WriteString(w, `{"valid":true,"name":"notes","template":"notes@1","limitations":[]}`)
			return
		}
		if r.URL.Path == "/v1/app-import-jobs" {
			imports++
			if r.Method != "POST" || r.Header.Get("Idempotency-Key") != "app-import-replay" {
				t.Error("incorrect import")
			}
		} else if r.URL.Path != "/v1/app-import-jobs/"+testID || r.Method != "GET" {
			t.Error("incorrect poll")
		}
		io.WriteString(w, `{"id":"`+testID+`","state":"`+states[calls]+`"}`)
		calls++
	})
	code, out, stderr := invoke(t, appArgs("validate"), appManifest)
	if code != 0 || stderr != "" || decode(t, out)["valid"] != true || calls != 0 {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = invoke(t, appArgs("import"), appManifest)
	if code != 0 || stderr != "" || decode(t, out)["state"] != "ready" || decode(t, out)["idempotency_key"] != "app-import-replay" || imports != 1 {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = invoke(t, []string{"apps", "status", testID, "--json"}, "")
	if code != 0 || stderr != "" || decode(t, out)["state"] != "ready" || calls != 5 {
		t.Fatal(code, out, stderr)
	}
}

func TestAppsFailureTimeoutAndUnknownState(t *testing.T) {
	for _, state := range []string{"failed", "validating", "mystery"} {
		t.Run(state, func(t *testing.T) {
			calls := 0
			worker(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				io.WriteString(w, `{"id":"`+testID+`","state":"`+state+`","error":"private arbitrary text"}`)
			})
			code, out, stderr := invoke(t, appArgs("import", "--wait-timeout", "2ms"), appManifest)
			value := decode(t, stderr)["error"].(map[string]any)
			want := map[string]string{"failed": "app_import_failed", "validating": "app_import_wait_timeout", "mystery": "invalid_response"}[state]
			if code == 0 || out != "" || value["code"] != want || value["job_id"] != testID || value["idempotency_key"] != "app-import-replay" || calls != 1 || strings.Contains(stderr, "private arbitrary") {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

func TestAppsNoWaitLostResponseAndConflict(t *testing.T) {
	var calls atomic.Int32
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		count := calls.Add(1)
		if count == 1 {
			io.WriteString(w, `{"id":"`+testID+`","state":"queued"}`)
			return
		}
		if count == 2 {
			io.Copy(io.Discard, r.Body)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
			return
		}
		w.WriteHeader(409)
		io.WriteString(w, `{"error":{"code":"idempotency_conflict"}}`)
	})
	code, out, stderr := invoke(t, appArgs("import", "--no-wait"), appManifest)
	if code != 0 || stderr != "" || decode(t, out)["state"] != "queued" || calls.Load() != 1 {
		t.Fatal(code, out, stderr)
	}
	for _, want := range []string{"transport_error", "idempotency_conflict"} {
		code, out, stderr = invoke(t, appArgs("import"), appManifest)
		value := decode(t, stderr)["error"].(map[string]any)
		if code != 1 || out != "" || value["code"] != want || value["idempotency_key"] != "app-import-replay" {
			t.Fatal(code, out, stderr)
		}
	}
	if calls.Load() != 3 {
		t.Fatal("automatic retries")
	}
}

func TestAppsMalformedInputsAndResponses(t *testing.T) {
	calls := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{"id":"`+strings.Repeat("c", 32)+`","state":"ready","valid":false}`)
	})
	for _, args := range [][]string{appArgs("import", "--idempotency-key", "bad"), {"apps", "status", "../bad"}, appArgs("validate", "--no-wait")} {
		code, _, _ := invoke(t, args, appManifest)
		if code != 2 {
			t.Fatal(code)
		}
	}
	code, _, _ := invoke(t, appArgs("import"), "[]")
	if code != 2 || calls != 0 {
		t.Fatal(code, calls)
	}
	for _, args := range [][]string{appArgs("validate"), {"apps", "status", testID, "--json"}} {
		code, out, stderr := invoke(t, args, appManifest)
		if code == 0 || out != "" || decode(t, stderr)["error"].(map[string]any)["code"] != "invalid_response" {
			t.Fatal(code, out, stderr)
		}
	}
}

func TestAppsNativeExecutable(t *testing.T) {
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/app-import-jobs" || r.Header.Get("Idempotency-Key") != "native-import" {
			t.Error("wrong native request")
		}
		io.WriteString(w, `{"id":"`+testID+`","state":"ready"}`)
	})
	binary := filepath.Join(t.TempDir(), "vlno")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/vlno")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	command := exec.Command(binary, "apps", "import", "--file", "-", "--idempotency-key", "native-import", "--json")
	command.Env = os.Environ()
	command.Stdin = strings.NewReader(appManifest)
	out, err := command.CombinedOutput()
	if err != nil || decode(t, string(out))["state"] != "ready" {
		t.Fatal(err, string(out))
	}
}
