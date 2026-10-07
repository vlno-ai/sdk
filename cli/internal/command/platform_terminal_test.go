package command

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestNullProductResultContract(t *testing.T) {
	raw, err := os.ReadFile("../../../testdata/terminal-without-result.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&cases); err != nil {
		t.Fatal(err)
	}
	for _, value := range cases {
		expected := value["valid"]
		delete(value, "valid")
		value["id"], value["engine"], value["result"] = productID, "closed_world", nil
		if got := validProductRun(value, productID); got != expected {
			t.Fatalf("state %v cancellation %v: got %v want %v", value["state"], value["cancellationRequested"], got, expected)
		}
	}
}

func TestUnallocatedCancellationStatusNextAndWait(t *testing.T) {
	for _, state := range []string{"cancelled", "failed"} {
		t.Run(state, func(t *testing.T) {
			worker(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v1/world-runs/"+productID {
					t.Error("terminal observation must not claim or mutate", r.Method, r.URL.Path)
				}
				json.NewEncoder(w).Encode(map[string]any{"id": productID, "engine": "closed_world",
					"state": state, "cancellationRequested": true, "result": nil})
			})
			t.Setenv("VLNO_API_KEY", productKey)
			dir := t.TempDir()
			connection := filepath.Join(dir, "connection.json")
			commands := []struct {
				args []string
				code int
			}{
				{[]string{"status", productID}, 0},
				{[]string{"next", productID, "--claim-file", filepath.Join(dir, "claim"), "--out", connection}, 0},
				{[]string{"wait", productID}, 1},
			}
			for _, command := range commands {
				args := append([]string{"--json", "platform", "runs"}, command.args...)
				code, out, stderr := invoke(t, args, "")
				if code != command.code || out == "" {
					t.Fatalf("%s: exit %d, output %s, error %s", command.args[0], code, out, stderr)
				}
			}
			if _, err := os.Stat(connection); !os.IsNotExist(err) {
				t.Fatal("terminal run must not export an empty connection")
			}
		})
	}
}

func TestCancellationDoesNotAcceptContradictoryWorkerResult(t *testing.T) {
	value := productView("running", "not_evaluated")
	value["state"], value["cancellationRequested"] = "cancelled", true
	if validProductRun(value, productID) {
		t.Fatal("contradictory nonnull result accepted")
	}
}
