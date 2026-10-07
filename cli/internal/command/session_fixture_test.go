package command

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type sessionAPI struct {
	mu                        sync.Mutex
	prep, receipt, status     map[string]any
	claim                     string
	posts, finishes, nexts    int
	loseAdmission, loseFinish bool
	loseOutput                bool
	events                    []map[string]any
}

func sessionServer(t *testing.T) *sessionAPI {
	t.Helper()
	f := &sessionAPI{

		prep: assessmentFixture(t, "preparation"),

		receipt: assessmentFixture(t, "admission"),

		status: assessmentFixture(t, "status"),
	}
	actor := map[string]any{"kind": "service_key", "reference": "sa_" + strings.Repeat("a", 32)}
	f.prep["actor"] = actor
	f.receipt["actor"] = actor
	f.status["admittedBy"] = actor
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+productKey {
			t.Error("bad account identity")
		}
		if r.Method == "POST" {
			f.posts++
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/access"):
			json.NewEncoder(w).Encode(map[string]any{

				"schema": "vlno.assessment-run-access/1",

				"orgId": f.prep["orgId"],

				"actor": actor,
			})
		case strings.HasSuffix(r.URL.Path, "/run-preparation"):
			json.NewEncoder(w).Encode(f.prep)
		case strings.HasSuffix(r.URL.Path, "/assessment-run"):
			json.NewEncoder(w).Encode(f.status)
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/world-runs/"):
			reply := productView("running", "not_evaluated")
			reply["id"] = f.receipt["runId"]
			json.NewEncoder(w).Encode(reply)
		case strings.HasSuffix(r.URL.Path, "/runs"):
			body, e := decodeObject(r.Body)
			if e != nil {
				t.Error(e)
			}
			f.receipt["submitted"] = body
			f.receipt["idempotencyKey"] = r.Header.Get("Idempotency-Key")
			if f.loseAdmission {
				f.loseAdmission = false
				dropReply(w)
				return
			}
			json.NewEncoder(w).Encode(f.receipt)
		case strings.HasSuffix(r.URL.Path, "/next"):
			f.nexts++
			body, _ := decodeObject(r.Body)
			claim := body["claim"].(string)
			if f.claim != "" && claim != f.claim {
				t.Error("claim changed")
			}
			f.claim = claim
			reply := productAssignment("ready")
			run := reply["run"].(map[string]any)
			run["id"] = f.receipt["runId"]
			run["result"].(map[string]any)["cases"].([]any)[0].(map[string]any)["scenario"] = "archive-note@1"
			reply["case"].(map[string]any)["scenario"] = "archive-note@1"
			json.NewEncoder(w).Encode(reply)
		case strings.HasSuffix(r.URL.Path, "/transcript"):
			body, _ := decodeObject(r.Body)
			event := body["events"].([]any)[0].(map[string]any)
			f.events = append(f.events, event)
			if f.loseOutput && event["kind"] == "stdout" {
				f.loseOutput = false
				dropReply(w)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"accepted": []any{event["id"]}})
		case strings.HasSuffix(r.URL.Path, "/finish"):
			f.finishes++
			f.status["phase"] = "finished"
			f.status["version"] = json.Number("2")
			f.status["cleanup"] = "confirmed"
			f.status["actions"] = map[string]any{"canClaim": false, "canCancel": false}
			f.status["evidence"] = map[string]any{

				"state": "available",

				"historyAvailable": true,

				"observationsAvailable": true,
			}
			f.status["outcome"] = map[string]any{

				"status": "passed",

				"assurance": "trusted_worker",

				"scope": "final_target_archived_and_peer_final_state",

				"semanticCheck": "verified_from_sealed_worker_receipts",

				"observationAuthority": "trusted_worker",
			}
			if f.loseFinish {
				f.loseFinish = false
				dropReply(w)
				return
			}
			reply := productView("completed", "passed")
			reply["id"] = f.receipt["runId"]
			json.NewEncoder(w).Encode(reply)
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			f.status["phase"] = "cancelled"
			reply := productView("cancelled", "not_evaluated")
			reply["id"] = f.receipt["runId"]
			reply["cancellationRequested"] = true
			json.NewEncoder(w).Encode(reply)
		default:
			t.Error("unexpected route", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	t.Setenv("VLNO_API_KEY", productKey)
	return f
}

type sessionCounts struct {
	posts, nexts, finishes int
	claim                  string
	events                 []map[string]any
}

func (f *sessionAPI) snapshot() sessionCounts {
	f.mu.Lock()
	defer f.mu.Unlock()
	return sessionCounts{
		posts:    f.posts,
		nexts:    f.nexts,
		finishes: f.finishes,

		claim:  f.claim,
		events: append([]map[string]any{}, f.events...),
	}
}
func dropReply(w http.ResponseWriter) {
	conn, _, e := w.(http.Hijacker).Hijack()
	if e == nil {
		conn.Close()
	}
}

func (f *sessionAPI) loseReply(kind string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch kind {
	case "finish":
		f.loseFinish = true
	case "admission":
		f.loseAdmission = true
	case "output":
		f.loseOutput = true
	}
}
