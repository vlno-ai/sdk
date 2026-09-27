package command

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestExecPreservesCommandArgumentsAfterSeparator(t *testing.T) {
	s := worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/worlds/"+testID+"/exec" {
			t.Error("wrong route")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		want := []any{"echo", "--endpoint", "http://not-a-worker.invalid", "--help", "--json"}
		if !reflect.DeepEqual(body["argv"], want) || body["cwd"] != "/home/model" {
			t.Error("command was rewritten", body)
		}
		json.NewEncoder(w).Encode(map[string]any{"exit_code": 0, "stdout": "ok", "stderr": "", "truncated": false})
	})
	code, _, stderr := invoke(t, []string{"--endpoint", s.URL, "--json", "worlds", "exec", testID, "--", "echo", "--endpoint", "http://not-a-worker.invalid", "--help", "--json"}, "")
	if code != 0 || stderr != "" {
		t.Fatal(code, stderr)
	}
}
