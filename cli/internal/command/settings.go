package command

import (
	"github.com/vlno-ai/sdk/cli/internal/api"
	"github.com/vlno-ai/sdk/cli/internal/config"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (r *runner) configPath() (string, error) {
	if r.opts.configPath != "" {
		return r.opts.configPath, nil
	}
	if p := os.Getenv("VLNO_CONFIG"); p != "" {
		return p, nil
	}
	p, e := config.DefaultPath()
	if e != nil {
		return "", fail("config_path_failed")
	}
	return p, nil
}

func (r *runner) settings() (config.Config, error) {
	p, e := r.configPath()
	if e != nil {
		return config.Config{}, e
	}
	c, e := config.Load(p)
	if e != nil {
		return c, fail("config_read_failed")
	}
	if v := os.Getenv("VLNO_ENDPOINT"); v != "" {
		c.Endpoint = v
	}
	if v := os.Getenv("VLNO_API_KEY"); v != "" {
		c.APIKey = v
	}
	if v := os.Getenv("VLNO_CA_FILE"); v != "" {
		c.CAFile = v
	}
	if r.opts.endpoint != "" {
		c.Endpoint = r.opts.endpoint
	}
	if r.opts.caFile != "" {
		c.CAFile = r.opts.caFile
	}
	return c, nil
}

func (r *runner) client() (*api.Client, error) {
	c, e := r.settings()
	if e != nil {
		return nil, e
	}
	if c.APIKey == "" {
		return nil, fail("missing_credentials")
	}
	if c.Endpoint == "" {
		return nil, fail("missing_endpoint")
	}
	return api.New(c.Endpoint, c.APIKey, c.CAFile, r.opts.timeout)
}

func (r *runner) login(args []string) error {
	return r.saveLogin(args, false)
}

func (r *runner) saveLogin(args []string, platform bool) error {
	fs := flags("login")
	stdin := fs.Bool("key-stdin", false, "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || !*stdin {
		return fail("usage")
	}
	c, e := r.settings()
	if e != nil {
		return e
	}
	data, e := io.ReadAll(io.LimitReader(r.in, 131))
	if e != nil || len(data) > 130 {
		return fail("invalid_credentials")
	}
	c.APIKey = strings.TrimSpace(string(data))
	if platform && !customerKey.MatchString(c.APIKey) {
		return fail("invalid_customer_key")
	}
	if c.Endpoint == "" {
		return fail("missing_endpoint")
	}
	a, e := api.New(c.Endpoint, c.APIKey, c.CAFile, r.opts.timeout)
	if e != nil {
		return e
	}
	path := "/v1/catalog"
	if platform {
		path = "/v1/world-runs"
	}
	if _, e = a.Request(r.ctx, "GET", path, nil, ""); e != nil {
		return e
	}
	p, e := r.configPath()
	if e != nil {
		return e
	}
	if c.CAFile != "" {
		absolute, err := filepath.Abs(c.CAFile)
		if err != nil {
			return fail("invalid_ca_file")
		}
		c.CAFile = absolute
	}
	if config.Save(p, c) != nil {
		return fail("config_save_failed")
	}
	return r.emit(map[string]any{"authenticated": true, "endpoint": c.Endpoint})
}
