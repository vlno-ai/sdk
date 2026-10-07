package session

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// PythonTransportSize models json.dumps with its default ASCII encoding and
// separators, including the single-event envelope used by recovery.
func PythonTransportSize(event map[string]any) int {
	return pythonSize(map[string]any{"claim": strings.Repeat("A", 43), "events": []any{event}})
}
func pythonSize(v any) int {
	switch x := v.(type) {
	case nil:
		return 4
	case bool:
		if x {
			return 4
		}
		return 5
	case string:
		n := 2
		for _, r := range x {
			switch {
			case r == '"' || r == '\\' || r == '\b' || r == '\f' || r == '\n' || r == '\r' || r == '\t':
				n += 2
			case r < 32 || r >= 127:
				if r > 65535 {
					n += 12
				} else {
					n += 6
				}
			default:
				n++
			}
		}
		return n
	case json.Number:
		return len(pythonNumber(x))
	case []any:
		n := 2
		for i, value := range x {
			if i > 0 {
				n += 2
			}
			n += pythonSize(value)
		}
		return n
	case map[string]any:
		n := 2
		i := 0
		for k, value := range x {
			if i > 0 {
				n += 2
			}
			n += pythonSize(k) + 2 + pythonSize(value)
			i++
		}
		return n
	default:
		return MaximumJournal
	}
}
func pythonNumber(n json.Number) string {
	s := string(n)
	if !strings.ContainsAny(s, ".eE") {
		return s
	}
	f, e := n.Float64()
	if e != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return strings.Repeat("!", MaximumJournal)
	}
	scientific := strconv.FormatFloat(f, 'e', -1, 64)
	parts := strings.Split(scientific, "e")
	exponent, _ := strconv.Atoi(parts[1])
	if exponent >= -4 && exponent < 16 {
		s = strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	}
	return scientific
}

// Floating JSON values use Python's shortest round-trip spelling for byte
// budgets. Integers retain their exact safe integer representation.
func portableNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		return json.Number(pythonNumber(x))
	case []any:
		a := make([]any, len(x))
		for i, item := range x {
			a[i] = portableNumbers(item)
		}
		return a
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, item := range x {
			m[k] = portableNumbers(item)
		}
		return m
	default:
		return v
	}
}
