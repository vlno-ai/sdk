package harness

import (
	"os"
	"strings"
)

func DeniedEnvironment(name string) bool {
	name = strings.ToUpper(name)
	return strings.HasPrefix(name, "VLNO_") || strings.HasPrefix(name, "CLOSED_WORLD_") || strings.HasPrefix(name, "WORKER_") ||
		strings.HasPrefix(name, "CW_") ||
		strings.Contains(name, "CLAIM")
}
func Environment(envKeys []string, privateValues ...string) []string {
	keys := append([]string{

		"PATH",

		"LANG",

		"TMPDIR",
	}, envKeys...)
	seen := map[string]bool{}
	result := []string{}
	for _, key := range keys {
		if seen[key] || DeniedEnvironment(key) {
			continue
		}
		seen[key] = true
		value, ok := os.LookupEnv(key)
		if !ok {
			continue
		}
		private := false
		for _, secret := range privateValues {
			if secret != "" && strings.Contains(value, secret) {
				private = true
				break
			}
		}
		if !private {
			result = append(result, key+"="+value)
		}
	}
	return result
}
