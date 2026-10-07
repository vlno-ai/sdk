package command

import (
	"encoding/json"
	"github.com/vlno-ai/sdk/cli/internal/config"
	"strings"
	"time"
)

// Reconstruct an allowlisted manifest, resolving only fixed gateway paths against
// the configured origin. Server fields cannot add an owner key or redirect URLs.
func productConnection(a, run map[string]any, s config.Config) (map[string]any, error) {
	invalid := func() (map[string]any, error) { return nil, fail("invalid_connection_manifest") }
	world, ok := a["worldId"].(string)
	if !ok || !worldID.MatchString(world) {
		return invalid()
	}
	task, ok := a["task"].(string)
	if !ok {
		return invalid()
	}
	index, ok := a["index"].(json.Number)
	if !ok {
		return invalid()
	}
	n, err := index.Int64()
	if err != nil || n < 0 || n > 15 {
		return invalid()
	}
	result, ok := run["result"].(map[string]any)
	if !ok {
		return invalid()
	}
	cases, ok := result["cases"].([]any)
	if !ok || int(n) >= len(cases) {
		return invalid()
	}
	record, ok := cases[n].(map[string]any)
	if !ok || record["scenario"] != a["scenario"] || record["state"] != "running" || run["state"] != "running" {
		return invalid()
	}
	expiry, ok := a["expiresAt"].(json.Number)
	if !ok {
		return invalid()
	}
	expires, err := expiry.Float64()
	if err != nil || expires <= float64(time.Now().Unix()) {
		return invalid()
	}
	manifest, ok := a["connection"].(map[string]any)
	if !ok || manifest["schema_version"] != json.Number("1") || manifest["generation"] != json.Number("1") || manifest["world_id"] != world || manifest["task"] != task || manifest["expires_at"] != expiry {
		return invalid()
	}
	clean := map[string]any{"schema_version": 1, "generation": 1, "world_id": world, "task": task, "expires_at": expiry, "ca_file": s.CAFile}
	token := ""
	for name, suffix := range map[string]string{"mcp": "mcp", "app": "call", "environment": "exec"} {
		entry, ok := manifest[name].(map[string]any)
		if !ok {
			if name == "environment" && manifest[name] == nil {
				continue
			}
			return invalid()
		}
		route := "/v1/world-agent/" + world + "/" + suffix
		if entry["path"] != route {
			return invalid()
		}
		if _, exists := entry["url"]; exists {
			return invalid()
		}
		headers, ok := entry["headers"].(map[string]any)
		if !ok || len(headers) != 1 {
			return invalid()
		}
		auth, ok := headers["Authorization"].(string)
		if !ok || !strings.HasPrefix(auth, "Bearer ") || !roleToken.MatchString(auth[7:]) || auth[7:] == s.APIKey || (token != "" && auth != token) {
			return invalid()
		}
		token = auth
		field := map[string]any{"url": strings.TrimRight(s.Endpoint, "/") + route, "headers": map[string]string{"Authorization": auth}}
		if name == "mcp" {
			if entry["transport"] != "streamable-http" {
				return invalid()
			}
			field["transport"] = "streamable-http"
		}
		if name == "environment" {
			field["transport"] = "exec-v1"
			field["cwd"] = "/home/model"
			field["network"] = "isolated-world"
		}
		clean[name] = field
	}
	return clean, nil
}
