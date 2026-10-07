package command

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

var worldID = regexp.MustCompile(`^[a-f0-9]{32}$`)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

var versionRef = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}@[1-9][0-9]{0,8}$`)

func replayKey(value string) (string, error) {
	if value != "" {
		if !keyPattern.MatchString(value) {
			return "", fail("invalid_idempotency_key")
		}
		return value, nil
	}
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", fail("random_failed")
	}
	return hex.EncodeToString(b), nil
}
