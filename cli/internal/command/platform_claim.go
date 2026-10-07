package command

import (
	"context"
	"io"
	"os"
	"strings"
	"time"
)

// Claims survive failed requests. Existing files must be private regular files;
// a new claim is flushed before the worker can observe it. Never print the claim.
func productClaim(path string, create bool) (string, error) {
	if create {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			defer f.Close()
			claim, e := replayKey("")
			if e != nil {
				os.Remove(path)
				return "", e
			}
			_, e = f.WriteString(claim + "\n")
			if e != nil || f.Sync() != nil || f.Close() != nil {
				os.Remove(path)
				return "", fail("claim_file_unavailable")
			}
			return claim, nil
		}
		if !os.IsExist(err) {
			return "", fail("claim_file_unavailable")
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 130 {
		return "", fail("invalid_claim_file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fail("invalid_claim_file")
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) || actual.Mode().Perm()&0077 != 0 {
		return "", fail("invalid_claim_file")
	}
	data, err := io.ReadAll(io.LimitReader(f, 131))
	claim := strings.TrimSpace(string(data))
	if err != nil || len(data) > 130 || !roleToken.MatchString(claim) {
		return "", fail("invalid_claim_file")
	}
	return claim, nil
}

func productPause(ctx context.Context) error {
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
