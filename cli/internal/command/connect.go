package command

import (
	"encoding/json"
	"os"
)

// Export the world capability only to an explicitly chosen new private file.
// The caller's configured origin is authoritative; server-provided URLs cannot
// redirect the capability to another host.
func (r *runner) connect(args []string) error {
	if len(args) < 1 || !worldID.MatchString(args[0]) {
		return fail("invalid_world_id")
	}
	id := args[0]
	fs := flags("connect")
	path := fs.String("out", "", "")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || *path == "" || *path == "-" {
		return fail("usage")
	}
	file, err := os.OpenFile(*path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail("connection_file_unavailable")
	}
	complete := false
	defer func() {
		file.Close()
		if !complete {
			os.Remove(*path)
		}
	}()
	if file.Chmod(0600) != nil {
		return fail("connection_file_unavailable")
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	settings, err := r.settings()
	if err != nil {
		return err
	}
	value, err := c.Request(r.ctx, "GET", "/v1/worlds/"+id+"/connection", nil, "")
	if err != nil {
		return err
	}
	if err = resolveConnection(value, id, settings.Endpoint, settings.CAFile, settings.APIKey); err != nil {
		return err
	}
	if json.NewEncoder(file).Encode(value) != nil || file.Sync() != nil || file.Close() != nil {
		return fail("connection_file_unavailable")
	}
	complete = true
	return r.emit(map[string]any{"world_id": id, "connection_file": *path, "expires_at": value["expires_at"]})
}

func resolveConnection(value map[string]any, id, endpoint, caFile, ownerKey string) error {
	if value["world_id"] != id || value["schema_version"] != json.Number("1") {
		return fail("invalid_response")
	}
	entries := map[string]string{"mcp": "mcp", "app": "call"}
	if _, ok := value["environment"]; ok {
		entries["environment"] = "exec"
	}
	for name, suffix := range entries {
		entry, ok := value[name].(map[string]any)
		if !ok || entry["path"] != "/v1/worlds/"+id+"/"+suffix {
			return fail("invalid_response")
		}
		headers, ok := entry["headers"].(map[string]any)
		if !ok || len(headers) != 1 {
			return fail("invalid_response")
		}
		auth, ok := headers["Authorization"].(string)
		if !ok || len(auth) < 7 || auth[:7] != "Bearer " || !roleToken.MatchString(auth[7:]) || auth[7:] == ownerKey {
			return fail("invalid_response")
		}
		entry["url"] = endpoint + "/v1/worlds/" + id + "/" + suffix
		delete(entry, "path")
	}
	value["ca_file"] = caFile
	return nil
}
