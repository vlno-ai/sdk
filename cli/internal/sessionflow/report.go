package sessionflow

// Report contains no command, event payloads, connection, claim or credentials.
func (p *Protocol) Report() (map[string]any, error) {
	j, e := p.read()
	if e != nil {
		return nil, e
	}
	capture := object(j, "capture")
	execution := object(j, "execution")
	report := map[string]any{

		"schema": "vlno.client-session-report/1",

		"sessionId": j["id"],

		"runId": runID(j),

		"admission": object(j, "admission")["state"],

		"claim": object(j, "claim")["state"],

		"process": execution["state"],

		"exitCode": execution["exitCode"],

		"capture": capture["state"],

		"pendingEvents": int64(len(capture["events"].([]any))) - integer(capture["acknowledgedThrough"]),

		"finish": object(j, "finish")["state"],

		"cancellation": object(j, "cancellation")["state"],

		"current": nil,

		"recovery": j["recovery"],
	}
	if observation := object(j, "observation"); observation != nil {
		value := object(observation, "value")
		report["current"] = map[string]any{

			"phase": value["phase"],

			"outcome": value["outcome"],

			"evidence": value["evidence"],

			"cleanup": value["cleanup"],

			"synchronization": value["synchronization"],

			"checkedAt": observation["checkedAt"],
		}
	}
	return report, nil
}
