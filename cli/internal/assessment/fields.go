// Package assessment validates the approval-bound public API contract.
package assessment

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"
)

var UUID = regexp.MustCompile(`^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)
var RunID = regexp.MustCompile(`^cw_[a-f0-9]{32}$`)
var ReplayKey = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func object(v any, fields string) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	names := strings.Fields(fields)
	if !ok || len(m) != len(names) {
		return nil, false
	}
	for _, name := range names {
		if _, exists := m[name]; !exists {
			return nil, false
		}
	}
	return m, true
}

func text(v any) string { s, _ := v.(string); return s }
func oneOf(v any, values string) bool {
	s, ok := v.(string)
	return ok && slices.Contains(strings.Fields(values), s)
}
func bounded(v any) bool { s := text(v); return len(s) > 0 && len(s) <= 256 }
func boolean(v any) bool { _, ok := v.(bool); return ok }
func integer(v any, min, max int64) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	i, err := n.Int64()
	return err == nil && i >= min && i <= max
}
func timestamp(v any) (time.Time, bool) {
	s := text(v)
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, len(s) <= 40 && err == nil
}
func actor(v any) bool {
	m, ok := object(v, "kind reference")
	return ok && oneOf(m["kind"], "human service_key") && bounded(m["reference"])
}
func source(v any) (map[string]any, bool) {
	m, ok := object(v, "assessmentId planRevisionId approvalReviewId systemId systemRevisionId templateId")
	if !ok {
		return nil, false
	}
	for _, value := range m {
		if !UUID.MatchString(text(value)) {
			return nil, false
		}
	}
	return m, true
}
func support(v any) bool {
	want := map[string]any{
		"kind": "application", "profile": "tiny-notes-external/1", "application": "Tiny Notes",
		"task": "archive-note@1", "transport": "api_proxy_only", "plannedAttempts": json.Number("1"),
		"identityAssurance": "declared_harness", "outcomeAssurance": "trusted_worker",
		"modelAttested": false, "externalProcessControlled": false, "externalSpendEnforced": false,
	}
	m, ok := v.(map[string]any)
	if !ok || len(m) != len(want) {
		return false
	}
	for key, expected := range want {
		// JSON values can contain slices or maps. Compare only their required primitive type.
		switch e := expected.(type) {
		case string:
			if actual, ok := m[key].(string); !ok || actual != e {
				return false
			}
		case bool:
			if actual, ok := m[key].(bool); !ok || actual != e {
				return false
			}
		case json.Number:
			if !integer(m[key], 1, 1) {
				return false
			}
		}
	}
	return true
}
