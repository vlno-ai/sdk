package command

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadyOnlyClaimRetriesExplicitPreparationWithSamePrivateClaim(t *testing.T) {
	claims := []string{}
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(productView("pending", "not_evaluated"))
			return
		}
		body, _ := decodeObject(r.Body)
		claims = append(claims, body["claim"].(string))
		if len(claims) == 1 {
			w.WriteHeader(503)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "run_preparing"}})
			return
		}
		json.NewEncoder(w).Encode(productAssignment("ready"))
	})
	t.Setenv("VLNO_API_KEY", productKey)
	dir := t.TempDir()
	claim, connection := filepath.Join(dir, "claim"), filepath.Join(dir, "connection.json")
	code, out, stderr := invoke(t, []string{"platform", "runs", "next", productID, "--claim-file", claim, "--out", connection, "--json"}, "")
	if code != 0 || stderr != "" || len(claims) != 2 || claims[0] != claims[1] {
		t.Fatal(code, stderr, len(claims))
	}
	stored, err := os.ReadFile(claim)
	if err != nil || strings.TrimSpace(string(stored)) != claims[0] {
		t.Fatal("claim not retained")
	}
	manifest, err := os.ReadFile(connection)
	if err != nil || strings.Contains(string(manifest), productKey) || strings.Contains(string(manifest), claims[0]) {
		t.Fatal("owner credential entered connection")
	}
	if strings.Contains(out, claims[0]) || strings.Contains(out, scopedKey) {
		t.Fatal("capability printed")
	}
	info, _ := os.Stat(connection)
	if info.Mode().Perm() != 0600 {
		t.Fatal("connection permissions")
	}
}

func TestClaimDoesNotRetryUnknownOrUnrelatedServiceFailure(t *testing.T) {
	for _, item := range []struct{ code, outcome string }{
		{"worker_unavailable", ""}, {"run_preparing", "unknown"},
	} {
		t.Run(item.code+item.outcome, func(t *testing.T) {
			posts := 0
			worker(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					json.NewEncoder(w).Encode(productView("pending", "not_evaluated"))
					return
				}
				posts++
				w.WriteHeader(503)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": item.code, "outcome": item.outcome}})
			})
			t.Setenv("VLNO_API_KEY", productKey)
			dir := t.TempDir()
			claim, out := filepath.Join(dir, "claim"), filepath.Join(dir, "connection")
			code, _, _ := invoke(t, []string{"platform", "runs", "next", productID, "--claim-file", claim, "--out", out}, "")
			if code != 1 || posts != 1 {
				t.Fatal(code, posts)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("partial connection survived")
			}
			if _, err := os.Stat(claim); err != nil {
				t.Fatal("recovery claim lost")
			}
		})
	}
}
