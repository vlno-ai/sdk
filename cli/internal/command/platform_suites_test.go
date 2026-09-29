package command

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestHostedSuiteCatalog(t *testing.T) {
	entry := map[string]any{"suite": "notes@1", "suiteSha256": strings.Repeat("a", 64),
		"title": "Notes", "description": "Archive the requested note.",
		"limitations": []string{"No broad security claim."}, "environmentAllowed": false,
		"harnesses": []string{}, "transports": []string{"app", "mcp"}}
	response := map[string]any{"items": []any{entry}}
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/world-suites" || r.Header.Get("Authorization") != "Bearer "+productKey {
			t.Error("wrong catalog request")
		}
		json.NewEncoder(w).Encode(response)
	})
	t.Setenv("VLNO_API_KEY", productKey)
	code, out, stderr := invoke(t, []string{"platform", "suites", "list", "--json"}, "")
	if code != 0 || !strings.Contains(out, "notes@1") || strings.Contains(out+stderr, productKey) {
		t.Fatal(code, out, stderr)
	}
	response = map[string]any{"items": []any{}}
	code, _, stderr = invoke(t, []string{"platform", "suites", "list", "--json"}, "")
	if code != 0 {
		t.Fatal(stderr)
	}
	entry["environmentAllowed"] = "true"
	response = map[string]any{"items": []any{entry}}
	code, _, stderr = invoke(t, []string{"platform", "suites", "list", "--json"}, "")
	if code == 0 || !strings.Contains(stderr, "invalid_suite_catalog") {
		t.Fatal(code, stderr)
	}
}
