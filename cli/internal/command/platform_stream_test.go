package command

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestProductWatchStreamsTrajectoryAndResumesExplicitCursor(t *testing.T) {
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Query().Get("after") != "7" {
			t.Error("watch mutated or lost cursor")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(event string, data any, id string) {
			b, _ := json.Marshal(data)
			if id != "" {
				fmt.Fprintf(w, "id: %s\n", id)
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		}
		emit("run", map[string]any{"schema": "vlno.world-stream/1", "run": productView("completed", "passed"), "readiness": []any{}}, "")
		emit("trajectory", map[string]any{"schema": "vlno.world-trajectory/1", "runId": productID, "events": []any{map[string]any{"sequence": 8, "caseIndex": 0, "source": "harness", "kind": "message", "data": map[string]any{"text": "recorded"}}}, "nextCursor": 8, "hasMore": false, "terminal": true, "settled": true}, "8")
		emit("done", map[string]any{"runId": productID, "nextCursor": 8}, "")
	})
	t.Setenv("VLNO_API_KEY", productKey)
	code, out, stderr := invoke(t, []string{"platform", "runs", "watch", productID, "--after", "7", "--json"}, "")
	if code != 0 || stderr != "" || len(strings.Split(strings.TrimSpace(out), "\n")) != 3 || !strings.Contains(out, "recorded") {
		t.Fatal(code, out, stderr)
	}
}
func TestProductWatchRejectsForeignRunOrCursorRegression(t *testing.T) {
	for _, invalid := range []string{"identity", "cursor"} {
		t.Run(invalid, func(t *testing.T) {
			worker(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				run := productView("running", "not_evaluated")
				if invalid == "identity" {
					run["id"] = "cw_" + strings.Repeat("f", 32)
				}
				b, _ := json.Marshal(map[string]any{"schema": "vlno.world-stream/1", "run": run, "readiness": []any{}})
				fmt.Fprintf(w, "event: run\ndata: %s\n\n", b)
				if invalid == "cursor" {
					fmt.Fprintf(w, "id: 6\nevent: trajectory\ndata: {\"runId\":%q,\"nextCursor\":6,\"events\":[]}\n\n", productID)
				}
			})
			t.Setenv("VLNO_API_KEY", productKey)
			code, _, stderr := invoke(t, []string{"platform", "runs", "watch", productID, "--after", "7", "--json"}, "")
			if code != 2 || !strings.Contains(stderr, "invalid_stream") {
				t.Fatal(code, stderr)
			}
		})
	}
}
