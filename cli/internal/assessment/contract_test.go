package assessment

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/assessment-" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.UseNumber()
	var value map[string]any
	if err = d.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestSharedClientContractFixtures(t *testing.T) {
	prep := fixture(t, "preparation")
	s := prep["source"].(map[string]any)
	if !Preparation(prep, text(s["assessmentId"]), text(s["planRevisionId"])) {
		t.Fatal("preparation")
	}
	admit := fixture(t, "admission")
	if !Admission(admit, text(s["assessmentId"]), admit["submitted"].(map[string]any), text(admit["idempotencyKey"])) {
		t.Fatal("admission")
	}
	status := fixture(t, "status")
	if !Status(status, text(status["id"])) {
		t.Fatal("status")
	}
	status["lastConfirmedAt"] = "2026-10-07T08:59:59.000Z"
	if !Status(status, text(status["id"])) {
		t.Fatal("wall clock corrections are not status corruption")
	}
}

func TestPreparationRejectsContradictionsAndForeignShapes(t *testing.T) {
	mutations := map[string]func(map[string]any){
		"duplicate group":            func(v map[string]any) { v["checks"].([]any)[1].(map[string]any)["group"] = "system" },
		"blocked outer state":        func(v map[string]any) { v["state"] = "blocked" },
		"capacity invented":          func(v map[string]any) { v["limits"].(map[string]any)["capacityReserved"] = true },
		"cost ceiling":               func(v map[string]any) { v["limits"].(map[string]any)["requestedMaxCostUsd"] = "100.01" },
		"array in primitive":         func(v map[string]any) { v["support"].(map[string]any)["profile"] = []any{} },
		"unexpected sensitive field": func(v map[string]any) { v["workerToken"] = "not-a-credential" },
	}
	for _, blocker := range []string{"scope_not_supported", "approval_required"} {
		mutations[blocker] = func(v map[string]any) {
			v["state"] = "blocked"
			c := v["checks"].([]any)[0].(map[string]any)
			c["state"], c["blockers"] = "blocked", []any{blocker}
		}
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			v := fixture(t, "preparation")
			s := v["source"].(map[string]any)
			mutate(v)
			if Preparation(v, text(s["assessmentId"]), text(s["planRevisionId"])) {
				t.Fatal("accepted invalid response")
			}
		})
	}
}

func TestAdmissionRequiresExactReceiptAndBoundedClaimWindow(t *testing.T) {
	for _, field := range []string{"idempotencyKey", "claimBefore", "submitted", "executionStarted", "reservedAttempts"} {
		t.Run(field, func(t *testing.T) {
			v := fixture(t, "admission")
			s := v["source"].(map[string]any)
			command := map[string]any{}
			for k, item := range v["submitted"].(map[string]any) {
				command[k] = item
			}
			key := text(v["idempotencyKey"])
			switch field {
			case "idempotencyKey":
				v[field] = "another-key"
			case "claimBefore":
				v[field] = "2026-10-07T09:10:01.000Z"
			case "submitted":
				v[field].(map[string]any)["approvalReviewId"] = s["systemId"]
			case "executionStarted":
				v[field] = true
			case "reservedAttempts":
				v[field] = json.Number("2")
			}
			if Admission(v, text(s["assessmentId"]), command, key) {
				t.Fatal("accepted changed receipt")
			}
		})
	}
}

func TestUnavailableEvidenceCannotClaimObservations(t *testing.T) {
	v := fixture(t, "status")
	v["synchronization"] = "unavailable"
	e := v["evidence"].(map[string]any)
	e["state"], e["historyAvailable"] = "incomplete", true
	if !Status(v, text(v["id"])) {
		t.Fatal("incomplete must remain readable")
	}
	e["observationsAvailable"] = true
	if Status(v, text(v["id"])) {
		t.Fatal("invented observations accepted")
	}
}

func TestStatusRejectsUnboundedClaimWindow(t *testing.T) {
	v := fixture(t, "status")
	v["claimBefore"] = "2026-10-07T09:10:01.000Z"
	if Status(v, text(v["id"])) {
		t.Fatal("unbounded claim window")
	}
}

func TestInconclusiveOutcomeCannotClaimVerifiedSemantics(t *testing.T) {
	v := fixture(t, "status")
	o := map[string]any{"status": "inconclusive", "assurance": "trusted_worker",
		"scope": "final_target_archived_and_peer_final_state", "observationAuthority": "trusted_worker",
		"semanticCheck": "verified_from_sealed_worker_receipts"}
	v["outcome"] = o
	if Status(v, text(v["id"])) {
		t.Fatal("inconclusive result claims verification")
	}
	o["semanticCheck"] = "not_available"
	if !Status(v, text(v["id"])) {
		t.Fatal("valid inconclusive result rejected")
	}
}
