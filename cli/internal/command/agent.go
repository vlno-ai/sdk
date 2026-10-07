package command

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/vlno-ai/sdk/cli/internal/harness"
	"io"
	"os"
	"regexp"
	"strings"
)

type agentConfig struct {
	Argv        []string `json:"argv"`
	EnvKeys     []string `json:"env_keys,omitempty"`
	InputFormat string   `json:"input_format,omitempty"`
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// This is trusted local configuration, never obtained from the worker or model.
func loadAgentConfig(path string) (agentConfig, error) {
	var cfg agentConfig
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return cfg, fail("invalid_agent_config")
	}
	f, err := os.Open(path)
	if err != nil {
		return cfg, fail("invalid_agent_config")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return cfg, fail("invalid_agent_config")
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(data) > 65536 {
		return cfg, fail("invalid_agent_config")
	}
	// Decode keys individually so duplicates cannot silently override argv/env_keys.
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return cfg, fail("invalid_agent_config")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] {
			return cfg, fail("invalid_agent_config")
		}
		seen[name] = true
		switch name {
		case "argv":
			err = d.Decode(&cfg.Argv)
		case "env_keys":
			err = d.Decode(&cfg.EnvKeys)
		case "input_format":
			err = d.Decode(&cfg.InputFormat)
		default:
			return cfg, fail("invalid_agent_config")
		}
		if err != nil {
			return cfg, fail("invalid_agent_config")
		}
	}
	if _, err = d.Token(); err != nil {
		return cfg, fail("invalid_agent_config")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return cfg, fail("invalid_agent_config")
	}
	if len(cfg.Argv) < 1 || len(cfg.Argv) > 128 || cfg.Argv[0] == "" || len(cfg.EnvKeys) > 64 {
		return cfg, fail("invalid_agent_config")
	}
	if cfg.InputFormat != "" && cfg.InputFormat != "connection-v1" {
		return cfg, fail("invalid_agent_config")
	}
	for _, a := range cfg.Argv {
		if len(a) > 8192 || strings.ContainsRune(a, 0) {
			return cfg, fail("invalid_agent_config")
		}
	}
	seen = map[string]bool{}
	for _, k := range cfg.EnvKeys {
		if !envName.MatchString(k) || seen[k] || deniedEnv(k) {
			return cfg, fail("invalid_agent_config")
		}
		seen[k] = true
	}
	return cfg, nil
}
func deniedEnv(name string) bool { return harness.DeniedEnvironment(name) }
func agentEnv(cfg agentConfig, privateValues ...string) []string {
	return harness.Environment(cfg.EnvKeys, privateValues...)
}

// Legacy suite execution deliberately discards output; durable sessions use the
// same process ownership primitive with an explicit bounded capture sink.
func executeAgent(ctx context.Context, cfg agentConfig, input map[string]any, env []string) string {
	data, err := json.Marshal(input)
	if err != nil {
		return "error"
	}
	return harness.Run(ctx, harness.Spec{Argv: cfg.Argv, Env: env, Input: data, Stdout: io.Discard, Stderr: io.Discard}, nil).Status
}
