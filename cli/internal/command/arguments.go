package command

import (
	"flag"
	"io"
	"strings"
	"time"
)

func globals(args []string) (options, []string, error) {
	o := options{timeout: 200 * time.Second, waitTimeout: 330 * time.Second}
	// Recognize --json even when another argument is invalid.
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "--json" {
			o.json = true
		}
	}
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		name, value, equal := strings.Cut(args[i], "=")
		if name == "--json" {
			if equal {
				return o, nil, fail("invalid_flag")
			}
			continue
		}
		switch name {
		case "--endpoint", "--ca-file", "--config", "--timeout", "--wait-timeout":
			if !equal {
				i++
				if i >= len(args) {
					return o, nil, fail("invalid_flag")
				}
				value = args[i]
			}
			if value == "" {
				return o, nil, fail("invalid_flag")
			}
			switch name {
			case "--endpoint":
				o.endpoint = value
			case "--ca-file":
				o.caFile = value
			case "--config":
				o.configPath = value
			default:
				d, e := time.ParseDuration(value)
				if e != nil || d <= 0 {
					return o, nil, fail("invalid_timeout")
				}
				if name == "--timeout" {
					o.timeout = d
				} else {
					o.waitTimeout = d
				}
			}
		default:
			rest = append(rest, args[i])
		}
	}
	return o, rest, nil
}

func flags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}
