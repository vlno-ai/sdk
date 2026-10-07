// Package session implements the shared private, local durable session store.
package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MaximumJournal = 2 * 1024 * 1024

var ErrCorrupt = errors.New("session_corrupt")

// Decode rejects JSON ambiguities before any state can authorize a side effect.
func Decode(data []byte) (map[string]any, error) {
	if len(data) > MaximumJournal || !utf8.Valid(data) || !validEscapes(data) {
		return nil, ErrCorrupt
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := readValue(d, 0)
	if err != nil {
		return nil, ErrCorrupt
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, ErrCorrupt
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, ErrCorrupt
	}
	return m, nil
}
func readValue(d *json.Decoder, depth int) (any, error) {
	if depth > 16 {
		return nil, ErrCorrupt
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch x := t.(type) {
	case json.Delim:
		if x == '{' {
			m := map[string]any{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return nil, e
				}
				key, ok := k.(string)
				if !ok {
					return nil, ErrCorrupt
				}
				if _, ok = m[key]; ok {
					return nil, ErrCorrupt
				}
				v, e := readValue(d, depth+1)
				if e != nil {
					return nil, e
				}
				m[key] = v
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return nil, ErrCorrupt
			}
			return m, nil
		}
		if x == '[' {
			a := []any{}
			for d.More() {
				v, e := readValue(d, depth+1)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return nil, ErrCorrupt
			}
			return a, nil
		}
		return nil, ErrCorrupt
	case json.Number:
		if !strings.ContainsAny(string(x), ".eE") {
			i, e := x.Int64()
			if e != nil || i < -9007199254740991 || i > 9007199254740991 {
				return nil, ErrCorrupt
			}
			return json.Number(strconv.FormatInt(i, 10)), nil
		}
		f, e := strconv.ParseFloat(string(x), 64)
		if e != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, ErrCorrupt
		}
		return json.Number(pythonNumber(x)), nil
	}
	return t, nil
}

// encoding/json replaces isolated UTF-16 surrogates; reject those first.
func validEscapes(b []byte) bool {
	inside := false
	for i := 0; i < len(b); i++ {
		if b[i] == '"' {
			inside = !inside
			continue
		}
		if !inside || b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return false
		}
		if b[i] != 'u' {
			continue
		}
		n, ok := hexCode(b, i+1)
		if !ok {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return false
			}
			low, ok := hexCode(b, i+3)
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return !inside
}
func hexCode(b []byte, start int) (uint64, bool) {
	if start+4 > len(b) {
		return 0, false
	}
	n, e := strconv.ParseUint(string(b[start:start+4]), 16, 16)
	return n, e == nil
}
