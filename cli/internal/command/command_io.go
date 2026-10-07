package command

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func decodeObject(reader io.Reader) (map[string]any, error) {
	data, e := io.ReadAll(io.LimitReader(reader, 65537))
	if e != nil || len(data) > 65536 {
		return nil, fail("invalid_json_input")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var result map[string]any
	if decoder.Decode(&result) != nil || result == nil {
		return nil, fail("invalid_json_input")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fail("invalid_json_input")
	}
	return result, nil
}

func (r *runner) readObject(path string) (map[string]any, error) {
	if path == "-" {
		return decodeObject(r.in)
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, fail("input_read_failed")
	}
	defer f.Close()
	return decodeObject(f)
}

func public(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for k, v := range value {
		if k != "agent_token" {
			out[k] = v
		}
	}
	return out
}

func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}

func (r *runner) emit(value any) error {
	e := json.NewEncoder(r.out)
	if !r.opts.json {
		e.SetIndent("", "  ")
	}
	return e.Encode(value)
}

func (r *runner) progress(label, value string) {
	if !r.opts.json {
		fmt.Fprintln(r.err, label, value)
	}
}
