package command

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

func assessmentFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	f, err := os.Open("../../../testdata/assessment-" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	v, err := decodeObject(f)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func admissionArgs(v map[string]any) []string {
	s := v["source"].(map[string]any)
	return []string{"platform", "assessments", "admit", s["assessmentId"].(string),
		"--revision", s["planRevisionId"].(string), "--approval", s["approvalReviewId"].(string),
		"--system-revision", s["systemRevisionId"].(string), "--idempotency-key", v["idempotencyKey"].(string)}
}

func TestAssessmentReadinessAndStatusDoNotExecute(t *testing.T) {
	prep, status := assessmentFixture(t, "preparation"), assessmentFixture(t, "status")
	source := prep["source"].(map[string]any)
	paths := []string{}
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) != 0 {
			t.Fatal("assessment reads must not send a JSON null body")
		}
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer "+productKey {
			t.Error("invalid read")
		}
		value := prep
		if strings.HasSuffix(r.URL.Path, "/assessment-run") {
			value = status
		}
		json.NewEncoder(w).Encode(value)
	})
	t.Setenv("VLNO_API_KEY", productKey)
	commands := [][]string{
		{"platform", "assessments", "prepare", source["assessmentId"].(string), "--revision", source["planRevisionId"].(string)},
		{"platform", "assessments", "status", status["id"].(string)},
	}
	for i, args := range commands {
		code, out, stderr := invoke(t, args, "")
		if code != 0 || stderr != "" || strings.Contains(out, "%!") || strings.Contains(out, "workerToken") {
			t.Fatal(code, out, stderr)
		}
		if i == 0 && !strings.Contains(out, "up to 300 seconds") {
			t.Fatal(out)
		}
	}
	want := []string{"GET /v1/assessments/" + source["assessmentId"].(string) + "/revisions/" + source["planRevisionId"].(string) + "/run-preparation", "GET /v1/world-runs/" + status["id"].(string) + "/assessment-run"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatal(paths)
	}
}

func TestAdmissionReplaysOnlyWhenCallerRepeatsSavedCommand(t *testing.T) {
	v := assessmentFixture(t, "admission")
	hits := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		body, err := decodeObject(r.Body)
		if err != nil || !reflect.DeepEqual(body, v["submitted"]) || r.Header.Get("Idempotency-Key") != v["idempotencyKey"] {
			t.Error("changed admission command")
		}
		if hits == 1 {
			w.WriteHeader(503)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "admission_outcome_unknown", "outcome": "unknown"}})
			return
		}
		json.NewEncoder(w).Encode(v)
	})
	t.Setenv("VLNO_API_KEY", productKey)
	args := append(admissionArgs(v), "--json")
	code, _, stderr := invoke(t, args, "")
	if code != 1 || hits != 1 || !strings.Contains(stderr, v["idempotencyKey"].(string)) {
		t.Fatal(code, hits, stderr)
	}
	code, out, stderr := invoke(t, args, "")
	if code != 0 || hits != 2 || stderr != "" || !strings.Contains(out, v["runId"].(string)) {
		t.Fatal(code, hits, out, stderr)
	}
}

func TestAdmissionRejectsChangedReceiptAndKeepsRecoveryKey(t *testing.T) {
	v := assessmentFixture(t, "admission")
	args := append(admissionArgs(v), "--json")
	v["submitted"].(map[string]any)["approvalReviewId"] = v["source"].(map[string]any)["systemId"]
	worker(t, func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(v) })
	t.Setenv("VLNO_API_KEY", productKey)
	code, out, stderr := invoke(t, args, "")
	if code != 1 || out != "" || !strings.Contains(stderr, `"outcome":"unknown"`) || !strings.Contains(stderr, "admission_outcome_unknown") || !strings.Contains(stderr, v["idempotencyKey"].(string)) {
		t.Fatal(code, out, stderr)
	}
}

func TestAssessmentArgumentsFailBeforeNetwork(t *testing.T) {
	hits := 0
	worker(t, func(w http.ResponseWriter, r *http.Request) { hits++ })
	for _, args := range [][]string{
		{"platform", "assessments", "prepare", "../foreign"},
		{"platform", "assessments", "status", "not-a-run"},
		{"platform", "assessments", "admit", "11111111-1111-4111-8111-111111111111"},
	} {
		code, _, _ := invoke(t, args, "")
		if code != 2 {
			t.Fatal(code, args)
		}
	}
	if hits != 0 {
		t.Fatal("invalid route reached server")
	}
}
