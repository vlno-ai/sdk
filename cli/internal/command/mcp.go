package command

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/vlno-ai/sdk/cli/internal/api"
)

// One protocol adapter for every stdio MCP client, without loading agent config,
// provider credentials, owner credentials, or any agent-specific integration.
func (r *runner) mcp(args []string) error {
	fs := flags("mcp")
	file := fs.String("connection", "", "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *file == "" {
		return fail("usage")
	}
	info, err := os.Lstat(*file)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 65536 {
		return fail("invalid_connection_file")
	}
	f, err := os.Open(*file)
	if err != nil {
		return fail("invalid_connection_file")
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return fail("invalid_connection_file")
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(data) > 65536 {
		return fail("invalid_connection_file")
	}
	var manifest struct {
		Schema  int    `json:"schema_version"`
		WorldID string `json:"world_id"`
		CAFile  string `json:"ca_file"`
		MCP     struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcp"`
	}
	if json.Unmarshal(data, &manifest) != nil || manifest.Schema != 1 || !worldID.MatchString(manifest.WorldID) {
		return fail("invalid_connection_file")
	}
	u, err := url.Parse(manifest.MCP.URL)
	if err != nil || u == nil || (u.Path != "/v1/worlds/"+manifest.WorldID+"/mcp" && u.Path != "/v1/world-agent/"+manifest.WorldID+"/mcp") || u.RawPath != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fail("invalid_connection_file")
	}
	auth := manifest.MCP.Headers["Authorization"]
	if len(manifest.MCP.Headers) != 1 || !strings.HasPrefix(auth, "Bearer ") {
		return fail("invalid_connection_file")
	}
	c, err := api.New(u.Scheme+"://"+u.Host, strings.TrimPrefix(auth, "Bearer "), manifest.CAFile, r.opts.timeout)
	if err != nil {
		return err
	}
	scan := bufio.NewScanner(r.in)
	scan.Buffer(make([]byte, 4096), api.MaxRequestBytes)
	out := json.NewEncoder(r.out)
	version := ""
	emitError := func(id any, code int, message string) error {
		return out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
	}
	for scan.Scan() {
		if r.ctx.Err() != nil {
			return r.ctx.Err()
		}
		d := json.NewDecoder(bytes.NewReader(scan.Bytes()))
		d.UseNumber()
		var request map[string]any
		if d.Decode(&request) != nil || request == nil {
			if emitError(nil, -32700, "Parse error") != nil {
				return fail("mcp_output_failed")
			}
			continue
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			if emitError(nil, -32700, "Parse error") != nil {
				return fail("mcp_output_failed")
			}
			continue
		}
		id, hasID := request["id"]
		method, valid := request["method"].(string)
		if request["jsonrpc"] != "2.0" || !valid {
			if emitError(nil, -32600, "Invalid Request") != nil {
				return fail("mcp_output_failed")
			}
			continue
		}
		response, e := c.MCP(r.ctx, u.Path, request, version)
		if e != nil {
			if hasID {
				if emitError(id, -32000, "World request failed; outcome may be unknown. No retry was made.") != nil {
					return fail("mcp_output_failed")
				}
			}
			continue
		}
		if !hasID {
			continue
		}
		if response == nil {
			if emitError(id, -32603, "Invalid server response") != nil {
				return fail("mcp_output_failed")
			}
			continue
		}
		if method == "initialize" {
			if result, ok := response["result"].(map[string]any); ok {
				v, ok := result["protocolVersion"].(string)
				if ok && regexp.MustCompile(`^2025-(11-25|06-18|03-26)$`).MatchString(v) {
					version = v
				}
			}
		}
		if out.Encode(response) != nil {
			return fail("mcp_output_failed")
		}
	}
	if scan.Err() != nil {
		return fail("mcp_input_failed")
	}
	return nil
}
