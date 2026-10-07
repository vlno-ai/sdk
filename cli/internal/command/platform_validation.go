package command

import (
	"encoding/json"
	"regexp"
	"strconv"
)

var productRunID = regexp.MustCompile(`^cw_[a-f0-9]{32}$`)

var customerKey = regexp.MustCompile(`^vlno_live_[A-Za-z0-9_-]{23,119}$`)

var productRevision = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/@:+-]{0,127}$`)

func validProductRun(v map[string]any, id string) bool {
	if v["id"] != id || v["engine"] != "closed_world" {
		return false
	}
	if v["state"] == "provisioning" {
		return v["result"] == nil
	}
	if !validRun(v, id) {
		return false
	}
	result, ok := v["result"].(map[string]any)
	if !ok || result["schema"] != "vlno.world-run/1" || result["engine"] != "closed_world" || result["executionState"] != v["state"] {
		return false
	}
	switch result["evaluationStatus"] {
	case "not_evaluated", "passed", "failed", "inconclusive", "invalid":
	default:
		return false
	}
	cases, ok := result["cases"].([]any)
	if !ok || len(cases) == 0 || len(cases) > 16 {
		return false
	}
	for i, raw := range cases {
		c, ok := raw.(map[string]any)
		if !ok || c["index"] != json.Number(strconv.Itoa(i)) {
			return false
		}
		scenario, ok := c["scenario"].(string)
		if !ok || !versionRef.MatchString(scenario) {
			return false
		}
	}
	return true
}
