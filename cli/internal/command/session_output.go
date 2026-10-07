package command

import (
	"errors"
	"fmt"
	"github.com/vlno-ai/sdk/cli/internal/api"
	"strings"
)

func sessionFailure(err error) error {
	var own *failure
	var remote *api.Error
	if errors.As(err, &own) || errors.As(err, &remote) {
		return err
	}
	value := err.Error()
	if strings.HasPrefix(value, "session_") || value == "outbox_full" || value == "connection_expired" ||
		value == "admission_outcome_unknown" ||
		value == "invalid_transcript_acknowledgement" {
		return fail(value)
	}
	return fail("session_operation_unavailable")
}
func (r *runner) showSession(path string, report map[string]any) error {
	if r.opts.json {
		return r.emit(report)
	}
	if report["admission"] == "prepared" {
		fmt.Fprintln(r.out, "Session prepared. No assessment run has been requested.")
		fmt.Fprintln(r.out, "Resume will not submit this prepared session. Start again in a new private directory.")
		return nil
	}
	if report["admission"] == "unknown" {
		fmt.Fprintln(r.out, "The run request may have been received. Its outcome is not yet confirmed.")
	}
	process := map[string]string{

		"not_started": "Your command has not started.",

		"start_unknown": "It is not known whether your command started.",

		"running": "Your command was last recorded as running.",

		"exit_unknown": "The previous process outcome is unknown. Resume will not start it again.",

		"external_active": "An external harness was connected; its process is not controlled here.",
	}
	if report["process"] == "exited" {
		fmt.Fprintf(r.out, "Your command exited (code %v). This is separate from the assessment result.\n", report["exitCode"])
	} else {
		fmt.Fprintln(r.out, process[fmt.Sprint(report["process"])])
	}
	capture := map[string]string{

		"not_started": "Output recording has not started.",

		"open": "Output recording has not finished.",

		"incomplete": "The recorded output is incomplete.",

		"complete": "The recorded output is complete.",
	}
	fmt.Fprintln(r.out, capture[fmt.Sprint(report["capture"])])
	fmt.Fprintf(r.out, "Recorded events awaiting upload: %v\n", report["pendingEvents"])
	if current, ok := report["current"].(map[string]any); ok {
		if current["synchronization"] == "unavailable" {
			fmt.Fprintln(r.out, "A current refresh is unavailable; the last observation may be stale.")
		} else if current["synchronization"] == "reconciling" {
			fmt.Fprintln(r.out, "The service is reconciling the run; this observation may change.")
		}
		if evidence, ok := current["evidence"].(map[string]any); ok {
			labels := map[string]string{

				"pending": "Not retained yet",

				"available": "Available",

				"incomplete": "Incomplete",

				"unavailable": "Unavailable",

				"removed": "No longer retained",
			}
			fmt.Fprintln(r.out, "Retained evidence:", labels[fmt.Sprint(evidence["state"])])
		}
		phases := map[string]string{

			"awaiting_harness": "Waiting for a harness",

			"preparing": "Preparing application access",

			"running": "Application access active",

			"collecting_evidence": "Retaining assessment evidence",

			"finished": "Assessment finished",

			"cancel_requested": "Cancellation requested",

			"cancelled": "Application access cancelled",

			"interrupted": "Application access interrupted",

			"expired": "Application access expired",
		}
		fmt.Fprintln(r.out, "Last observed:", phases[fmt.Sprint(current["phase"])])
		if outcome, ok := current["outcome"].(map[string]any); ok {
			labels := map[string]string{

				"passed": "Passed",

				"failed": "Criterion not met",

				"inconclusive": "Inconclusive",

				"invalid": "Could not be validated",

				"error": "Could not be assessed",

				"not_run": "Not run",
			}
			fmt.Fprintln(r.out, "Assessment:", labels[fmt.Sprint(outcome["status"])])
		} else {
			fmt.Fprintln(r.out, "Assessment result is not available yet.")
		}
		cleanup := map[string]string{

			"not_allocated": "No application allocation",

			"pending": "Pending",

			"confirmed": "Confirmed",

			"failed": "Needs attention",
		}
		fmt.Fprintln(r.out, "Application cleanup:", cleanup[fmt.Sprint(current["cleanup"])])
	}
	if report["runId"] != "" {
		fmt.Fprintln(r.out, "Run reference:", report["runId"])
	}
	if report["finish"] == "accepted" && report["capture"] == "complete" && report["pendingEvents"] == int64(0) {
		fmt.Fprintf(r.out, "To check the retained result: vlno sessions status %s\n", quoteSessionPath(path))
		return nil
	}
	fmt.Fprintf(r.out, "To check or recover saved requests: vlno sessions resume %s\n", quoteSessionPath(path))
	fmt.Fprintln(r.out, "Resume never starts your command again.")
	return nil
}

func quoteSessionPath(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'"
}
