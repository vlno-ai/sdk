package command

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformRecorderRetainsUnicodeAndRedactsSplitControllerSecrets(t *testing.T) {
	claim := strings.Repeat("c", 40)
	content := ""
	events := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/world-runs/"+productID+"/cases/0/transcript" || r.Header.Get("Authorization") != "Bearer "+productKey {
			t.Error("wrong upload route or credential")
		}
		var body struct {
			Claim  string `json:"claim"`
			Events []struct {
				ID   string         `json:"id"`
				Kind string         `json:"kind"`
				Data map[string]any `json:"data"`
			} `json:"events"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Claim != claim || len(body.Events) != 1 {
			t.Fatal("invalid upload")
		}
		event := body.Events[0]
		events++
		if event.Kind == "stdout" {
			content += event.Data["text"].(string)
		}
		json.NewEncoder(w).Encode(map[string]any{"accepted": []string{event.ID}})
	})
	t.Setenv("VLNO_API_KEY", productKey)
	path := filepath.Join(t.TempDir(), "claim")
	if err := os.WriteFile(path, []byte(claim), 0600); err != nil {
		t.Fatal(err)
	}
	input := strings.Repeat("é", 2040) + productKey + " " + strings.Repeat("🌏", 2040) + claim + "\n"
	code, out, err := invoke(t, []string{"platform", "runs", "record", productID, "--case", "0", "--claim-file", path, "--json"}, input)
	if code != 0 {
		t.Fatal(code, out, err)
	}
	expected := strings.ReplaceAll(strings.ReplaceAll(input, productKey, "[redacted]"), claim, "[redacted]")
	if content != expected {
		t.Fatal("stream changed or a split secret escaped redaction")
	}
	if events < 4 {
		t.Fatal("expected bounded chunks and lifecycle markers")
	}
}

func TestPlatformTranscriptCursorUsesTheAuthenticatedReadRoute(t *testing.T) {
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/world-runs/"+productID+"/cases/0/transcript" || r.URL.RawQuery != "after=42" || r.Header.Get("Authorization") != "Bearer "+productKey {
			t.Error("wrong transcript read")
		}
		json.NewEncoder(w).Encode(map[string]any{"schema": "vlno.world-transcript/1", "runId": productID, "caseIndex": 0, "events": []any{}, "nextCursor": 42, "hasMore": false, "terminal": true})
	})
	t.Setenv("VLNO_API_KEY", productKey)
	code, out, err := invoke(t, []string{"platform", "runs", "transcript", productID, "--case", "0", "--after", "42", "--json"}, "")
	if code != 0 {
		t.Fatal(code, out, err)
	}
}
