package command

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const productID = "cw_" + testID

var productKey = "vlno_live_" + strings.Repeat("a", 48)
var scopedKey = strings.Repeat("s", 40)

func productView(state, outcome string) map[string]any {
	caseState := "running"
	cleanup := "pending"
	if state == "completed" {
		caseState = outcome
		cleanup = "confirmed"
	}
	return map[string]any{"id": productID, "engine": "closed_world", "state": state, "suite": "notes-suite@1", "revision": "test/revision",
		"result": map[string]any{"schema": "vlno.world-run/1", "engine": "closed_world", "executionState": state, "evaluationStatus": outcome,
			"cases":   []any{map[string]any{"index": 0, "scenario": "notes-case@1", "state": caseState}},
			"summary": map[string]any{"cleanup": map[string]any{"status": cleanup}}}}
}
func productAssignment(state string) map[string]any {
	expiry := time.Now().Unix() + 600
	return map[string]any{"run": productView("running", "not_evaluated"), "case": map[string]any{
		"index": 0, "scenario": "notes-case@1", "worldId": testID, "state": state, "task": "Archive a test note", "expiresAt": expiry,
		"connection": map[string]any{"schema_version": 1, "generation": 1, "world_id": testID, "expires_at": expiry, "task": "Archive a test note",
			"mcp": map[string]any{"path": "/v1/world-agent/" + testID + "/mcp", "transport": "streamable-http", "headers": map[string]any{"Authorization": "Bearer " + scopedKey}},
			"app": map[string]any{"path": "/v1/world-agent/" + testID + "/call", "headers": map[string]any{"Authorization": "Bearer " + scopedKey}}}}}
}
func TestProductLifecycleUsesOwnerForControlAndScopedMCP(t *testing.T) {
	claims := []string{}
	finished := false
	mcpCalls := 0
	s := worker(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/world-agent/") {
			if r.Header.Get("Authorization") != "Bearer "+scopedKey || r.Header.Get("Accept") != "application/json, text/event-stream" {
				t.Error("incorrect scoped MCP request")
			}
			mcpCalls++
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"tools": []any{}}})
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+productKey {
			t.Error("missing customer key")
		}
		switch r.URL.Path {
		case "/v1/world-runs":
			if r.Method != "POST" || r.Header.Get("Idempotency-Key") != "stable-key-123" {
				t.Error("missing replay key")
			}
			var payload map[string]any
			json.NewDecoder(r.Body).Decode(&payload)
			if payload["suite"] != "notes-suite@1" || payload["revision"] != "test/revision" {
				t.Error("wrong create contract")
			}
			json.NewEncoder(w).Encode(productView("pending", "not_evaluated"))
		case "/v1/world-runs/" + productID:
			if finished {
				json.NewEncoder(w).Encode(productView("completed", "passed"))
			} else {
				json.NewEncoder(w).Encode(productView("running", "not_evaluated"))
			}
		case "/v1/world-runs/" + productID + "/events":
			w.Header().Set("Content-Type", "text/event-stream")
			data, _ := json.Marshal(map[string]any{"schema": "vlno.world-stream/1", "run": productView("running", "not_evaluated"), "readiness": []any{map[string]any{"caseIndex": 0, "worldId": testID, "state": "ready"}}})
			fmt.Fprintf(w, "event: run\ndata: %s\n\n", data)
		case "/v1/world-runs/" + productID + "/next":
			var payload map[string]any
			json.NewDecoder(r.Body).Decode(&payload)
			claims = append(claims, payload["claim"].(string))
			state := "ready"
			if len(claims) == 1 {
				state = "creating"
			}
			json.NewEncoder(w).Encode(productAssignment(state))
		case "/v1/world-runs/" + productID + "/finish":
			var payload map[string]any
			json.NewDecoder(r.Body).Decode(&payload)
			if payload["claim"] != claims[0] || payload["case_index"] != float64(0) || payload["agent_status"] != "completed" {
				t.Error("wrong finish claim")
			}
			finished = true
			json.NewEncoder(w).Encode(productView("completed", "passed"))
		default:
			t.Error("unexpected route", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	t.Setenv("VLNO_API_KEY", productKey)
	dir := t.TempDir()
	claimPath := filepath.Join(dir, "claim")
	path := filepath.Join(dir, "connection.json")
	commands := [][]string{
		{"platform", "runs", "create", "notes-suite@1", "--revision", "test/revision", "--idempotency-key", "stable-key-123"},
		{"platform", "runs", "next", productID, "--claim-file", claimPath, "--out", path},
	}
	for _, args := range commands {
		code, out, err := invoke(t, append(args, "--json"), "")
		if code != 0 || strings.Contains(out+err, scopedKey) || strings.Contains(out+err, productKey) {
			t.Fatal(code, out, err)
		}
	}
	if len(claims) != 1 {
		t.Fatal("readiness must not repeat case claims")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	manifest := decode(t, string(raw))
	if manifest["mcp"].(map[string]any)["url"] != s.URL+"/v1/world-agent/"+testID+"/mcp" || strings.Contains(string(raw), productKey) {
		t.Fatal("incorrect exported capability")
	}
	for _, file := range []string{path, claimPath} {
		info, _ := os.Stat(file)
		if info.Mode().Perm() != 0600 {
			t.Fatal("insecure file", file)
		}
	}
	// A generic MCP subprocess does not need (or read) customer credentials.
	t.Setenv("VLNO_API_KEY", "")
	t.Setenv("VLNO_ENDPOINT", "")
	code, out, err := invoke(t, []string{"mcp", "--connection", path}, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n")
	if code != 0 || mcpCalls != 1 || !strings.Contains(out, "tools") || err != "" {
		t.Fatal(code, out, err)
	}
	t.Setenv("VLNO_API_KEY", productKey)
	t.Setenv("VLNO_ENDPOINT", s.URL)
	for _, args := range [][]string{{"platform", "runs", "finish", productID, "--case", "0", "--claim-file", claimPath}, {"platform", "runs", "wait", productID}} {
		code, out, err := invoke(t, append(args, "--json"), "")
		if code != 0 || strings.Contains(out+err, claims[0]) {
			t.Fatal(code, out, err)
		}
	}
}

func TestProductLostAssignmentKeepsClaimWithoutImplicitRetry(t *testing.T) {
	var posts atomic.Int32
	var original atomic.Value
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(productView("running", "not_evaluated"))
			return
		}
		attempt := posts.Add(1)
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		claim := payload["claim"].(string)
		if attempt == 1 {
			original.Store(claim)
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		if claim != original.Load().(string) {
			t.Error("lost claim")
		}
		json.NewEncoder(w).Encode(productAssignment("ready"))
	})
	t.Setenv("VLNO_API_KEY", productKey)
	dir := t.TempDir()
	claim := filepath.Join(dir, "claim")
	outPath := filepath.Join(dir, "connection")
	args := []string{"platform", "runs", "next", productID, "--claim-file", claim, "--out", outPath, "--json"}
	code, out, err := invoke(t, args, "")
	if code == 0 || posts.Load() != 1 || strings.Contains(out+err, original.Load().(string)) {
		t.Fatal(code, out, err, posts.Load())
	}
	if _, e := os.Stat(outPath); !os.IsNotExist(e) {
		t.Fatal("partial connection file left")
	}
	if _, e := os.Stat(claim); e != nil {
		t.Fatal("claim missing after lost response")
	}
	code, out, err = invoke(t, args, "")
	if code != 0 || posts.Load() != 2 {
		t.Fatal(code, out, err, posts.Load())
	}
	code, _, _ = invoke(t, args, "")
	if code == 0 || posts.Load() != 2 {
		t.Fatal("overwrote credential or mutated after file conflict")
	}
}

func TestProductRejectsInvalidAssignments(t *testing.T) {
	for _, kind := range []string{"foreign_url", "wrong_world", "owner_key", "different_token", "extra_header", "expired", "wrong_case", "wrong_task"} {
		t.Run(kind, func(t *testing.T) {
			worker(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					json.NewEncoder(w).Encode(productView("running", "not_evaluated"))
					return
				}
				v := productAssignment("ready")
				a := v["case"].(map[string]any)
				m := a["connection"].(map[string]any)
				entry := m["app"].(map[string]any)
				switch kind {
				case "foreign_url":
					entry["url"] = "https://other.test/call"
				case "wrong_world":
					entry["path"] = "/v1/world-agent/" + strings.Repeat("f", 32) + "/call"
				case "owner_key":
					entry["headers"] = map[string]any{"Authorization": "Bearer " + productKey}
				case "different_token":
					entry["headers"] = map[string]any{"Authorization": "Bearer " + strings.Repeat("d", 40)}
				case "extra_header":
					entry["headers"].(map[string]any)["X-Owner-Key"] = productKey
				case "expired":
					a["expiresAt"] = 1
					m["expires_at"] = 1
				case "wrong_case":
					a["scenario"] = "wrong-case@1"
				case "wrong_task":
					m["task"] = "wrong task"
				}
				json.NewEncoder(w).Encode(v)
			})
			t.Setenv("VLNO_API_KEY", productKey)
			dir := t.TempDir()
			outPath := filepath.Join(dir, "connection")
			code, out, err := invoke(t, []string{"platform", "runs", "next", productID, "--claim-file", filepath.Join(dir, "claim"), "--out", outPath, "--json"}, "")
			if code == 0 || strings.Contains(out+err, productKey) || strings.Contains(out+err, scopedKey) {
				t.Fatal(code, out, err)
			}
			if _, e := os.Stat(outPath); !os.IsNotExist(e) {
				t.Fatal("invalid capability persisted")
			}
		})
	}
}

func TestProductWaitDoesNotConvertUnknownResultsIntoPassing(t *testing.T) {
	for _, outcome := range []string{"passed", "inconclusive", "invalid", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			worker(t, func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(productView("completed", outcome))
			})
			t.Setenv("VLNO_API_KEY", productKey)
			code, out, err := invoke(t, []string{"platform", "runs", "wait", productID, "--json"}, "")
			want := 1
			if outcome == "passed" {
				want = 0
			}
			if code != want || err != "" || !strings.Contains(out, outcome) {
				t.Fatal(code, out, err)
			}
		})
	}
}

func TestProductLoginUsesProductAPI(t *testing.T) {
	calls := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/world-runs" || r.Header.Get("Authorization") != "Bearer "+productKey {
			t.Error("wrong login route")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}, "total": 0})
	})
	code, out, err := invoke(t, []string{"platform", "login", "--key-stdin", "--json"}, productKey+"\n")
	if code != 0 || calls != 1 || strings.Contains(out+err, productKey) {
		t.Fatal(code, out, err)
	}
	code, _, _ = invoke(t, []string{"platform", "login", "--key-stdin"}, testKey+"\n")
	if code == 0 || calls != 1 {
		t.Fatal("accepted worker token")
	}
}

func TestProductRejectsExposedOrSymlinkedClaimBeforeMutation(t *testing.T) {
	calls := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) })
	t.Setenv("VLNO_API_KEY", productKey)
	dir := t.TempDir()
	claim := filepath.Join(dir, "claim")
	os.WriteFile(claim, []byte(strings.Repeat("c", 32)), 0644)
	for _, path := range []string{claim, filepath.Join(dir, "link")} {
		if path != claim {
			os.Symlink(claim, path)
		}
		code, _, _ := invoke(t, []string{"platform", "runs", "next", productID, "--claim-file", path, "--out", filepath.Join(dir, "connection")}, "")
		if code == 0 || calls != 0 {
			t.Fatal("accepted unsafe claim file")
		}
	}
}
