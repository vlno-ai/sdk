package session

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"regexp"
)

var claimText = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// ValidateClaim binds the separate private claim file without copying its secret.
func ValidateClaim(data []byte, journal map[string]any) error {
	if len(data) > 1024 {
		return ErrCorrupt
	}
	m, e := Decode(data)
	if e != nil {
		return ErrCorrupt
	}
	m, ok := object(m, "schema sessionId claim")
	if !ok || m["schema"] != "vlno.client-session-claim/1" || m["sessionId"] != journal["id"] || !claimText.MatchString(text(m["claim"])) {
		return ErrCorrupt
	}
	value, e := base64.RawURLEncoding.Strict().DecodeString(text(m["claim"]))
	if e != nil || len(value) != 32 {
		return ErrCorrupt
	}
	h := sha256.Sum256([]byte(text(m["claim"])))
	if hex.EncodeToString(h[:]) != part(journal, "claim")["sha256"] {
		return ErrCorrupt
	}
	return nil
}
