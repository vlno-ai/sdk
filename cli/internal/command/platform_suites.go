package command

import (
	"context"
	"encoding/json"
	"regexp"
	"unicode/utf8"
)

var hostedHarnessName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

func (r *runner) productSuites() error {
	settings, err := r.settings()
	if err != nil {
		return err
	}
	if !customerKey.MatchString(settings.APIKey) {
		return fail("invalid_customer_key")
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	value, err := c.Request(ctx, "GET", "/v1/world-suites", nil, "")
	if err != nil {
		return err
	}
	var catalog struct {
		Items []struct {
			Suite              string   `json:"suite"`
			Digest             string   `json:"suiteSha256"`
			Title              string   `json:"title"`
			Description        string   `json:"description"`
			Limitations        []string `json:"limitations"`
			EnvironmentAllowed *bool    `json:"environmentAllowed"`
			Harnesses          []string `json:"harnesses"`
			Transports         []string `json:"transports"`
		} `json:"items"`
	}
	data, err := json.Marshal(value)
	if err != nil || json.Unmarshal(data, &catalog) != nil || catalog.Items == nil || len(catalog.Items) > 128 {
		return fail("invalid_suite_catalog")
	}
	seen := map[string]bool{}
	for _, s := range catalog.Items {
		if !versionRef.MatchString(s.Suite) || !artifactDigest.MatchString(s.Digest) || seen[s.Suite] || s.EnvironmentAllowed == nil || len(s.Title) < 1 || utf8.RuneCountInString(s.Title) > 120 || len(s.Description) < 1 || utf8.RuneCountInString(s.Description) > 2000 || len(s.Limitations) < 1 || len(s.Limitations) > 16 || s.Harnesses == nil || len(s.Harnesses) > 16 || len(s.Transports) != 2 || s.Transports[0] != "app" || s.Transports[1] != "mcp" {
			return fail("invalid_suite_catalog")
		}
		if len(s.Harnesses) > 0 && !*s.EnvironmentAllowed {
			return fail("invalid_suite_catalog")
		}
		for _, name := range s.Harnesses {
			if !hostedHarnessName.MatchString(name) {
				return fail("invalid_suite_catalog")
			}
		}
		for _, text := range s.Limitations {
			if len(text) < 1 || utf8.RuneCountInString(text) > 500 {
				return fail("invalid_suite_catalog")
			}
		}
		seen[s.Suite] = true
	}
	return r.emit(value)
}
