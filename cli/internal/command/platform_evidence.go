package command

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strconv"

	"github.com/vlno-ai/sdk/cli/internal/api"
)

type productArtifact struct {
	CaseIndex    int    `json:"caseIndex"`
	Kind         string `json:"kind"`
	SHA256       string `json:"sha256"`
	EvaluationID string `json:"evaluationId"`
	State        string `json:"state"`
	Size         *int   `json:"size"`
}

var artifactKind = regexp.MustCompile(`^(report|snapshot|evidence|trace)$`)
var artifactDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (r *runner) productEvidence(c *api.Client, id string, index int, kind, out string) error {
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	path := "/v1/world-runs/" + id + "/evidence"
	value, err := c.Request(ctx, "GET", path, nil, "")
	if err != nil {
		return runError(err, id, "")
	}
	if value["schema"] != "vlno.world-evidence/1" || value["runId"] != id {
		return fail("invalid_evidence_manifest")
	}
	var manifest struct {
		Status           string            `json:"status"`
		EvidenceComplete *bool             `json:"evidenceComplete"`
		Artifacts        []productArtifact `json:"artifacts"`
	}
	data, err := json.Marshal(value)
	if err != nil || json.Unmarshal(data, &manifest) != nil || manifest.Artifacts == nil || len(manifest.Artifacts) > 64 || manifest.EvidenceComplete == nil {
		return fail("invalid_evidence_manifest")
	}
	if manifest.Status != "pending" && manifest.Status != "complete" && manifest.Status != "unavailable" {
		return fail("invalid_evidence_manifest")
	}
	if manifest.Status == "complete" && len(manifest.Artifacts) == 0 || manifest.Status == "unavailable" && len(manifest.Artifacts) != 0 {
		return fail("invalid_evidence_manifest")
	}
	rawArtifacts, ok := value["artifacts"].([]any)
	if !ok {
		return fail("invalid_evidence_manifest")
	}
	seen := map[string]bool{}
	var selected *productArtifact
	for i := range manifest.Artifacts {
		a := &manifest.Artifacts[i]
		raw, ok := rawArtifacts[i].(map[string]any)
		if !ok || raw["caseIndex"] != json.Number(strconv.Itoa(a.CaseIndex)) {
			return fail("invalid_evidence_manifest")
		}
		identity := strconv.Itoa(a.CaseIndex) + "/" + a.Kind
		if a.CaseIndex < 0 || a.CaseIndex > 15 || !artifactKind.MatchString(a.Kind) || !artifactDigest.MatchString(a.SHA256) || !worldID.MatchString(a.EvaluationID) || seen[identity] {
			return fail("invalid_evidence_manifest")
		}
		seen[identity] = true
		if a.State == "archived" {
			if a.Size == nil || *a.Size < 1 || *a.Size > api.MaxArtifactBytes {
				return fail("invalid_evidence_manifest")
			}
		} else if a.State != "pending" || a.Size != nil || manifest.Status == "complete" {
			return fail("invalid_evidence_manifest")
		}
		if a.CaseIndex == index && a.Kind == kind {
			selected = a
		}
	}
	if out == "" {
		return r.emit(value)
	}
	if selected == nil {
		return fail("artifact_unavailable")
	}
	if selected.State != "archived" {
		return fail("artifact_not_archived")
	}
	data, err = c.Artifact(ctx, path+"/"+strconv.Itoa(index)+"/"+kind, *selected.Size, selected.SHA256)
	if err != nil {
		return runError(err, id, "")
	}
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail("artifact_file_unavailable")
	}
	complete := false
	defer func() {
		f.Close()
		if !complete {
			os.Remove(out)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return fail("artifact_file_unavailable")
	}
	if f.Sync() != nil || f.Close() != nil {
		return fail("artifact_file_unavailable")
	}
	complete = true
	return r.emit(map[string]any{"run_id": id, "case_index": index, "kind": kind, "sha256": selected.SHA256, "bytes": len(data), "artifact_file": out})
}
