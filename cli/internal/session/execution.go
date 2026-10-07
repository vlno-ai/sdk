package session

import (
	"path/filepath"
	"regexp"
	"strings"
)

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validCommand(v any) bool {
	m, ok := object(v, "argv envKeys cwd")
	if !ok {
		return false
	}
	args, ok := m["argv"].([]any)
	if !ok || len(args) < 1 || len(args) > 64 {
		return false
	}
	total := 0
	for _, v := range args {
		s, ok := v.(string)
		if !ok || len(s) > 4096 || strings.ContainsRune(s, 0) {
			return false
		}
		total += len(s)
	}
	if text(args[0]) == "" || total > 32768 {
		return false
	}
	keys, ok := m["envKeys"].([]any)
	if !ok || len(keys) > 64 {
		return false
	}
	seen := map[string]bool{}
	for _, v := range keys {
		s := text(v)
		u := strings.ToUpper(s)
		if !envName.MatchString(s) || seen[s] || strings.HasPrefix(u, "VLNO_") || strings.HasPrefix(u, "CLOSED_WORLD_") || strings.HasPrefix(u, "CW_") || strings.HasPrefix(u, "WORKER_") || strings.Contains(u, "CLAIM") {
			return false
		}
		seen[s] = true
	}
	cwd := text(m["cwd"])
	return len(cwd) <= 4096 && !strings.ContainsRune(cwd, 0) && filepath.IsAbs(cwd)
}
func validExecution(v any, c map[string]any) bool {
	m, ok := object(v, "mode state launchId command pid exitCode finishedAt")
	if !ok || !one(m["mode"], "supervised external") {
		return false
	}
	state := text(m["state"])
	if m["mode"] == "external" {
		return one(state, "not_started external_active") && m["command"] == nil && m["launchId"] == nil && m["pid"] == nil && m["exitCode"] == nil && m["finishedAt"] == nil && (state == "not_started" || c["case"] != nil)
	}
	if !validCommand(m["command"]) || !one(state, "not_started start_unknown running exit_unknown exited") {
		return false
	}
	if state == "not_started" {
		return m["launchId"] == nil && m["pid"] == nil && m["exitCode"] == nil && m["finishedAt"] == nil
	}
	if c["case"] == nil || !uuid4.MatchString(text(m["launchId"])) {
		return false
	}
	if m["pid"] != nil && !integer(m["pid"], 1, 9007199254740991) {
		return false
	}
	if state == "start_unknown" && m["pid"] != nil || state == "running" && m["pid"] == nil {
		return false
	}
	if state == "exited" {
		return m["pid"] != nil && integer(m["exitCode"], -2147483648, 2147483647) && timestamp(m["finishedAt"])
	}
	return m["exitCode"] == nil && m["finishedAt"] == nil
}
