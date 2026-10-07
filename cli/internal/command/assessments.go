package command

import (
	"github.com/vlno-ai/sdk/cli/internal/assessment"
)

func (r *runner) assessments(args []string) error {
	if len(args) < 2 {
		return fail("usage")
	}
	action, id := args[0], args[1]
	if action == "status" {
		if len(args) != 2 || !assessment.RunID.MatchString(id) {
			return fail("invalid_run_id")
		}
	} else if !assessment.UUID.MatchString(id) {
		return fail("invalid_assessment_id")
	}
	fs := flags("platform assessments")
	revision, approval, system, key := "", "", "", ""
	switch action {
	case "admit":
		fs.StringVar(&approval, "approval", "", "")
		fs.StringVar(&system, "system-revision", "", "")
		fs.StringVar(&key, "idempotency-key", "", "")
		fallthrough
	case "prepare":
		fs.StringVar(&revision, "revision", "", "")
	case "status":
	default:
		return fail("usage")
	}
	if fs.Parse(args[2:]) != nil || fs.NArg() != 0 {
		return fail("usage")
	}
	if action != "status" && !assessment.UUID.MatchString(revision) {
		return fail("invalid_plan_revision")
	}
	if action == "admit" && (!assessment.UUID.MatchString(approval) || !assessment.UUID.MatchString(system) || !assessment.ReplayKey.MatchString(key)) {
		return fail("invalid_admission_command")
	}
	settings, err := r.settings()
	if err != nil {
		return err
	}
	if !customerKey.MatchString(settings.APIKey) {
		return fail("invalid_customer_key")
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	path, method := "/v1/assessments/"+id+"/revisions/"+revision+"/run-preparation", "GET"
	var body map[string]any
	if action == "status" {
		path = "/v1/world-runs/" + id + "/assessment-run"
	}
	if action == "admit" {
		path, method = "/v1/assessments/"+id+"/runs", "POST"
		body = map[string]any{"planRevisionId": revision, "approvalReviewId": approval, "systemRevisionId": system}
	}
	value, err := c.Request(r.ctx, method, path, body, key)
	if err != nil {
		return lifecycleError(err, "", key)
	}
	valid := false
	switch action {
	case "prepare":
		valid = assessment.Preparation(value, id, revision)
	case "admit":
		valid = assessment.Admission(value, id, body, key)
	case "status":
		valid = assessment.Status(value, id)
	}
	if !valid {
		if action == "admit" {
			return &failure{Code: "admission_outcome_unknown", Outcome: "unknown", Key: key}
		}
		return lifecycleError(fail("invalid_assessment_response"), "", key)
	}
	return r.showAssessment(action, value)
}
