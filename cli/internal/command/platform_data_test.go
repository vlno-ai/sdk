package command

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestHostedDataLifecycle(t *testing.T) {
	id := "cw_" + strings.Repeat("a", 32)
	status := map[string]any{"runId": id, "state": "deleting", "requestedAt": "2026-09-29T12:00:00Z", "workerDeletedAt": nil, "archiveDeletedAt": nil, "databaseDeletedAt": nil, "backupExpiryEstimate": nil, "expiresAt": nil, "canRequestDeletion": false, "externalCopiesManaged": false, "backupDeletionIndividuallyVerified": false}
	calls := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+productKey {
			t.Error("wrong credential")
		}
		if r.URL.Path == "/v1/world-data/policy" {
			if r.Method == "PUT" {
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				if body["retentionDays"] != nil || body["expectedRevision"] != float64(0) {
					t.Error(body)
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"retentionDays": nil, "revision": 0, "appliesTo": "new_runs", "retentionStartsAt": "run_terminal", "externalCopiesManaged": false})
			return
		}
		if r.URL.Path != "/v1/world-runs/"+id+"/data/deletion" || r.Method != "POST" {
			t.Error("wrong path or method")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["confirmRunId"] != id {
			t.Error("wrong confirmation")
		}
		json.NewEncoder(w).Encode(status)
	})
	t.Setenv("VLNO_API_KEY", productKey)
	code, _, _ := invoke(t, []string{"platform", "data", "delete", id}, "")
	if code == 0 || calls != 0 {
		t.Fatal("unconfirmed deletion")
	}
	for _, args := range [][]string{{"platform", "data", "policy"}, {"platform", "data", "set-policy", "--days", "keep", "--revision", "0"}, {"platform", "data", "delete", id, "--confirm-run", id}} {
		code, out, stderr := invoke(t, args, "")
		if code != 0 || strings.Contains(out+stderr, productKey) {
			t.Fatal(code, out, stderr)
		}
	}
	status["state"] = "live_data_deleted"
	code, out, stderr := invoke(t, []string{"platform", "data", "delete", id, "--confirm-run", id}, "")
	if code == 0 || !strings.Contains(stderr, "invalid_data_status") {
		t.Fatal(code, out, stderr)
	}
}
