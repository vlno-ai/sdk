package command

import (
	"fmt"
	"github.com/vlno-ai/sdk/cli/internal/config"
)

func (r *runner) execute(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, e := fmt.Fprint(r.out, help)
		return e
	}
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "--help" || a == "-h" {
			_, e := fmt.Fprint(r.out, help)
			return e
		}
	}
	if args[0] == "version" || args[0] == "--version" {
		if len(args) != 1 {
			return fail("usage")
		}
		return r.emit(map[string]any{"version": Version})
	}
	// Validate top-level routes before accessing config or the network.
	switch args[0] {
	case "sessions":
		return r.sessions(args[1:])
	case "platform":
		return r.platform(args[1:])
	case "login":
		return r.login(args[1:])
	case "logout":
		if len(args) != 1 {
			return fail("usage")
		}
		p, e := r.configPath()
		if e != nil {
			return e
		}
		if config.Delete(p) != nil {
			return fail("config_delete_failed")
		}
		return r.emit(map[string]any{"logged_out": true})
	case "catalog":
		return r.catalog(args[1:])
	case "worlds":
		return r.worlds(args[1:])
	case "mcp":
		return r.mcp(args[1:])
	case "scenarios":
		return r.scenarios(args[1:])
	case "authoring":
		return r.authoring(args[1:])
	case "apps":
		return r.apps(args[1:])
	case "suites":
		return r.suites(args[1:])
	case "runs":
		return r.runs(args[1:])
	default:
		return fail("usage")
	}
}
