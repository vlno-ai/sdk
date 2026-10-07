package sessionflow

import (
	"regexp"
	"strings"
)

var bearer = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]+`)
var controlKey = regexp.MustCompile(`vlno_live_[A-Za-z0-9_-]+`)
var secretField = regexp.MustCompile(`(?i)^(authorization|cookie|set-cookie|password|passwd|secret|api[-_]?key|access[-_]?token|refresh[-_]?token|token|claim)$`)

func scrubText(s string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return controlKey.ReplaceAllString(bearer.ReplaceAllString(s, "Bearer [redacted]"), "[redacted]")
}

// Scrub rejects collisions rather than silently dropping a redacted key.
func Scrub(v any, secrets []string) (any, error) {
	switch x := v.(type) {
	case string:
		return scrubText(x, secrets), nil
	case map[string]any:
		result := map[string]any{}
		for key, value := range x {
			k := scrubText(key, secrets)
			if _, ok := result[k]; ok {
				return nil, code("session_redaction_collision")
			}
			if secretField.MatchString(key) {
				result[k] = "[redacted]"
				continue
			}
			clean, e := Scrub(value, secrets)
			if e != nil {
				return nil, e
			}
			result[k] = clean
		}
		return result, nil
	case []any:
		result := make([]any, len(x))
		for i, value := range x {
			clean, e := Scrub(value, secrets)
			if e != nil {
				return nil, e
			}
			result[i] = clean
		}
		return result, nil
	default:
		return v, nil
	}
}
