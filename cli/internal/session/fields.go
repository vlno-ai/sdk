package session

import (
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var uuid = regexp.MustCompile(`^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)
var uuid4 = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
var hex32 = regexp.MustCompile(`^[a-f0-9]{32}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var person = regexp.MustCompile(`^sa_[a-f0-9]{32}$`)

func text(v any) string { s, _ := v.(string); return s }
func object(v any, fields string) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	names := strings.Fields(fields)
	if !ok || len(m) != len(names) {
		return nil, false
	}
	for _, n := range names {
		if _, ok = m[n]; !ok {
			return nil, false
		}
	}
	return m, true
}
func one(v any, values string) bool {
	for _, s := range strings.Fields(values) {
		if text(v) == s {
			return true
		}
	}
	return false
}
func integer(v any, min, max int64) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	i, e := n.Int64()
	return e == nil && i >= min && i <= max
}
func number(v any) int64 { n, _ := v.(json.Number); i, _ := n.Int64(); return i }
func timestamp(v any) bool {
	s := text(v)
	t, e := time.Parse("2006-01-02T15:04:05.000Z", s)
	return e == nil && t.Year() >= 1 && t.Format("2006-01-02T15:04:05.000Z") == s
}
func actor(v any) bool {
	m, ok := object(v, "kind reference")
	return ok && one(m["kind"], "human service_key") && person.MatchString(text(m["reference"]))
}
func boolean(v any) bool { _, ok := v.(bool); return ok }
func bounded(v any, n int) bool {
	s, ok := v.(string)
	return ok && s != "" && utf8.RuneCountInString(s) <= n
}

// Origin is shared with Python: one credential-free canonical HTTP origin.
func Origin(s string) (string, error) {
	if s == "" || strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r > 127 }) >= 0 {
		return "", ErrCorrupt
	}
	u, e := url.Parse(s)
	if e != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || strings.ContainsAny(s, "?#") || u.Opaque != "" || !(u.Path == "" || u.Path == "/") {
		return "", ErrCorrupt
	}
	scheme := strings.ToLower(u.Scheme)
	h := strings.ToLower(u.Hostname())
	if scheme != "http" && scheme != "https" {
		return "", ErrCorrupt
	}
	if strings.ContainsAny(u.Host, "[]") && net.ParseIP(h) == nil {
		return "", ErrCorrupt
	}
	if net.ParseIP(h) == nil {
		if len(h) > 253 || h == "" {
			return "", ErrCorrupt
		}
		for _, part := range strings.Split(h, ".") {
			if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
				return "", ErrCorrupt
			}
			for _, r := range part {
				if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
					return "", ErrCorrupt
				}
			}
		}
	}
	if scheme == "http" && h != "localhost" && h != "127.0.0.1" && h != "::1" {
		return "", ErrCorrupt
	}
	port := u.Port()
	if port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return "", ErrCorrupt
		}
		port = strconv.Itoa(n)
	} else if strings.HasSuffix(u.Host, ":") {
		return "", ErrCorrupt
	}
	if scheme == "https" && port == "443" || scheme == "http" && port == "80" {
		port = ""
	}
	if strings.Contains(h, ":") {
		h = "[" + h + "]"
	}
	if port != "" {
		h += ":" + port
	}
	return scheme + "://" + h, nil
}
