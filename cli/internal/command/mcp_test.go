package command

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericMCPConnectorProtocolAndNoAmbientCredentials(t *testing.T) {
	calls := 0
	token := strings.Repeat("s", 40)
	s := worker(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Accept") != "application/json, text/event-stream" {
			t.Error("wrong scope or transport")
		}
		var value map[string]any
		json.NewDecoder(r.Body).Decode(&value)
		if value["method"] == "notifications/initialized" {
			w.WriteHeader(202)
			return
		}
		if value["method"] == "initialize" {
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": value["id"], "result": map[string]any{"protocolVersion": "2025-11-25"}})
		} else {
			if r.Header.Get("MCP-Protocol-Version") != "2025-11-25" {
				t.Error("missing negotiation")
			}
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": value["id"], "result": map[string]any{"tools": []any{}}})
		}
	})
	path := filepath.Join(t.TempDir(), "connection.json")
	raw, _ := json.Marshal(map[string]any{"schema_version": 1, "world_id": testID, "mcp": map[string]any{"url": s.URL + "/v1/worlds/" + testID + "/mcp", "headers": map[string]string{"Authorization": "Bearer " + token}}})
	os.WriteFile(path, raw, 0600)
	input := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n"
	code, out, stderr := invoke(t, []string{"mcp", "--connection", path}, input)
	if code != 0 || stderr != "" || calls != 3 || len(strings.Split(strings.TrimSpace(out), "\n")) != 2 || strings.Contains(out+stderr, token) || strings.Contains(out+stderr, testKey) {
		t.Fatal(code, out, stderr, calls)
	}
	os.Chmod(path, 0644)
	code, _, _ = invoke(t, []string{"mcp", "--connection", path}, input)
	if code == 0 || calls != 3 {
		t.Fatal("accepted exposed credential file")
	}
}
