package command

import (
	"fmt"
	"strings"
)

func (r *runner) showAssessment(action string, value map[string]any) error {
	if r.opts.json {
		return r.emit(value)
	}
	var text strings.Builder
	switch action {
	case "prepare":
		fmt.Fprintf(&text, "Run preparation: %s\n", value["state"])
		for _, item := range value["checks"].([]any) {
			check := item.(map[string]any)
			fmt.Fprintf(&text, "  %s: %s\n", check["group"], check["state"])
			for _, blocker := range check["blockers"].([]any) {
				fmt.Fprintf(&text, "    %s\n", strings.ReplaceAll(blocker.(string), "_", " "))
			}
		}
		access := value["admissionAccess"].(map[string]any)
		if access["canAdmit"] == false {
			text.WriteString("Your current access cannot start this run.\n")
		}
		limits := value["limits"].(map[string]any)
		fmt.Fprintf(&text, "App access: up to %s seconds after claim. Capacity is not reserved.\n", limits["maximumAppAccessSeconds"])
		text.WriteString("Your external harness process and model spending are not controlled by VLNO.\n")
	case "admit":
		fmt.Fprintf(&text, "Run reserved: %s\nClaim before: %s\n", value["runId"], value["claimBefore"])
		text.WriteString("The environment has not started. Connect your harness using:\n")
		fmt.Fprintf(&text, "  vlno platform runs next %s --claim-file <private-claim-file> --out <new-connection-file>\n", value["runId"])
	case "status":
		fmt.Fprintf(&text, "Run: %s\nStatus: %s\n", value["id"], strings.ReplaceAll(value["phase"].(string), "_", " "))
		fmt.Fprintf(&text, "Synchronization: %s\nCleanup: %s\n", value["synchronization"], strings.ReplaceAll(value["cleanup"].(string), "_", " "))
		if outcome, ok := value["outcome"].(map[string]any); ok {
			fmt.Fprintf(&text, "Result: %s (trusted worker evidence)\n", outcome["status"])
			text.WriteString("Scope: target note archived; peer note's final text and archived state unchanged.\n")
		}
		evidence := value["evidence"].(map[string]any)
		fmt.Fprintf(&text, "Evidence: %s\nLast confirmed: %s\n", evidence["state"], value["lastConfirmedAt"])
	}
	_, err := fmt.Fprint(r.out, text.String())
	return err
}
