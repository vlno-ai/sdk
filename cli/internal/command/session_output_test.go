package command

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestSessionRecoveryPathIsLiteralShellArgument(t *testing.T) {
	for _, path := range []string{

		"plain",

		"has space",

		"a'b",

		"$(printf WRONG)`printf WRONG`",

		"a\nb",
	} {
		out, e := exec.Command("/bin/sh", "-c", "printf '%s' "+quoteSessionPath(path)).Output()
		if e != nil || string(out) != path {
			t.Fatal("path changed", path, string(out), e)
		}
	}
}

func TestSessionOutputSeparatesPendingEvidenceAndStaleObservation(t *testing.T) {
	for _, sync := range []string{"unavailable", "reconciling"} {
		var output bytes.Buffer
		r := &runner{out: &output}
		report := map[string]any{

			"admission": "confirmed",

			"process": "exit_unknown",

			"capture": "incomplete",

			"pendingEvents": int64(3),

			"runId": "cw_example",

			"current": map[string]any{

				"phase": "collecting_evidence",

				"synchronization": sync,

				"evidence": map[string]any{"state": "pending"},

				"cleanup": "pending",
			},
		}
		if e := r.showSession("local", report); e != nil {
			t.Fatal(e)
		}
		for _, text := range []string{

			"awaiting upload: 3",

			"Not retained yet",

			"Resume never starts",

			"not available yet",
		} {
			if !strings.Contains(output.String(), text) {
				t.Fatal(output.String())
			}
		}
		if !strings.Contains(output.String(), map[string]string{"unavailable": "refresh is unavailable", "reconciling": "service is reconciling"}[sync]) {
			t.Fatal(output.String())
		}
	}
}
