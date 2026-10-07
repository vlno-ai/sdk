//go:build darwin || linux

package session

import (
	"encoding/json"
	"strconv"
)

// Update uses revision validation to reject a concurrent stale local mutation.
func (s *Store) Update(change func(map[string]any) error) error {
	j, e := s.Read()
	if e != nil {
		return e
	}
	if e = change(j); e != nil {
		return e
	}
	j["revision"] = json.Number(strconv.FormatInt(number(j["revision"])+1, 10))
	j["updatedAt"] = Now()
	b, e := Encode(j)
	if e != nil {
		return e
	}
	return s.Save(b)
}

// PrivateClaim is for the trusted controller only, never a public report.
func (s *Store) PrivateClaim() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.readJournal()
	if e != nil {
		return "", e
	}
	b, e := s.read("claim.json", 1024)
	if e != nil {
		return "", e
	}
	if e = ValidateClaim(b, j); e != nil {
		return "", e
	}
	m, e := Decode(b)
	return text(m["claim"]), e
}
