package command

import (
	"fmt"
	"sort"
	"text/tabwriter"
)

func (r *runner) catalog(args []string) error {
	if len(args) == 0 {
		return fail("usage")
	}
	kind, tail := args[0], args[1:]
	fs := flags("catalog")
	var filter *string
	filterField, missing := "", ""
	switch kind {
	case "harnesses":
		missing = "harness_not_found"
	case "apps":
		missing = "app_not_found"
	case "worlds":
		filter = fs.String("app", "", "")
		filterField, missing = "app", "world_template_not_found"
	case "scenarios":
		filter = fs.String("world", "", "")
		filterField, missing = "template", "scenario_not_found"
	case "families":
		missing = "scenario_family_not_found"
	case "suites":
		missing = "suite_not_found"
	default:
		return fail("usage")
	}
	ref := ""
	if len(tail) > 0 && tail[0] == "show" {
		if len(tail) < 2 {
			return fail("usage")
		}
		ref, tail = tail[1], tail[2:]
	}
	if fs.Parse(tail) != nil {
		return fail("usage")
	}
	if fs.NArg() != 0 {
		if ref != "" || fs.NArg() != 2 || fs.Arg(0) != "show" {
			return fail("usage")
		}
		ref = fs.Arg(1)
	}
	c, e := r.client()
	if e != nil {
		return e
	}
	value, e := c.Request(r.ctx, "GET", "/v1/catalog", nil, "")
	if e != nil {
		return e
	}
	entries, ok := value[kind].(map[string]any)
	if !ok {
		return fail("invalid_response")
	}
	selected := make(map[string]any)
	for name, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			return fail("invalid_response")
		}
		if filter == nil || *filter == "" || entry[filterField] == *filter {
			selected[name] = entry
		}
	}
	if ref != "" {
		entry, ok := selected[ref]
		if !ok {
			return fail(missing)
		}
		return r.emit(entry)
	}
	if r.opts.json {
		return r.emit(map[string]any{kind: selected})
	}
	w := tabwriter.NewWriter(r.out, 0, 4, 2, ' ', 0)
	switch kind {
	case "harnesses":
		fmt.Fprintln(w, "HARNESS\tPROVIDER\tIMAGE")
	case "apps":
		fmt.Fprintln(w, "APP\tCONTRACT\tIMAGE")
	case "worlds":
		fmt.Fprintln(w, "WORLD\tAPP\tDESCRIPTION")
	case "scenarios":
		fmt.Fprintln(w, "SCENARIO\tWORLD\tDESCRIPTION")
	case "families":
		fmt.Fprintln(w, "FAMILY\tCOMPILER\tDESCRIPTION")
	case "suites":
		fmt.Fprintln(w, "SUITE\tJOB\tHASH")
	}
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := selected[name].(map[string]any)
		second, third := entry[filterField], entry["description"]
		if kind == "harnesses" {
			second, third = entry["provider"], entry["image_id"]
		} else if kind == "apps" {
			contract, _ := entry["app"].(map[string]any)
			second, third = contract["id"], entry["image_id"]
		} else if kind == "families" {
			second = entry["compiler_version"]
		} else if kind == "suites" {
			second, third = entry["job_id"], entry["suite_sha256"]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", safeText(name), safeText(fmt.Sprint(second)), safeText(fmt.Sprint(third)))
	}
	return w.Flush()
}
