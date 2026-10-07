//go:build darwin || linux

package session

import (
	"encoding/json"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func createStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session")
	s, e := Create(path, fixture(t, "prepared"), fixture(t, "claim"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}
func TestStoreDurableReplacementAndExclusiveLock(t *testing.T) {
	s, path := createStore(t)
	before, e := os.Stat(filepath.Join(path, "lock"))
	if e != nil {
		t.Fatal(e)
	}
	if other, e := Open(path, false); e == nil {
		other.Close()
		t.Fatal("second owner accepted")
	}
	j, e := s.Read()
	if e != nil {
		t.Fatal(e)
	}
	part(j, "admission")["state"] = "unknown"
	nextRevision(j)
	if e = s.Save(encoded(t, j)); e != nil {
		t.Fatal(e)
	}
	if e = s.Save(encoded(t, j)); e == nil {
		t.Fatal("stale revision saved")
	}
	after, e := os.Stat(filepath.Join(path, "lock"))
	if e != nil || !os.SameFile(before, after) {
		t.Fatal("lock inode replaced")
	}
	s.Close()
	next, e := Open(path, false)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	got, e := next.Read()
	if e != nil || number(got["revision"]) != 2 || part(got, "admission")["state"] != "unknown" {
		t.Fatal("durable update missing", e)
	}
	if _, e = Create(path, fixture(t, "prepared"), fixture(t, "claim")); e == nil {
		t.Fatal("existing session reset")
	}
}
func TestUnsafeFilesAndPartialCreationFailClosed(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "mode", "fifo", "missing claim", "wrong claim", "directory moved", "lock replaced"} {
		t.Run(kind, func(t *testing.T) {
			s, path := createStore(t)
			journal := filepath.Join(path, "journal.json")
			switch kind {
			case "symlink":
				os.Rename(journal, journal+".old")
				os.Symlink(journal+".old", journal)
			case "hardlink":
				os.Link(journal, journal+".link")
			case "mode":
				os.Chmod(journal, 0644)
			case "fifo":
				os.Remove(journal)
				if e := unix.Mkfifo(journal, 0600); e != nil {
					t.Fatal(e)
				}
			case "missing claim":
				os.Remove(filepath.Join(path, "claim.json"))
			case "wrong claim":
				os.WriteFile(filepath.Join(path, "claim.json"), []byte(`{}`), 0600)
			case "directory moved":
				os.Rename(path, path+".moved")
				os.Mkdir(path, 0700)
			case "lock replaced":
				os.Rename(filepath.Join(path, "lock"), filepath.Join(path, "old-lock"))
				os.WriteFile(filepath.Join(path, "lock"), nil, 0600)
			}
			if _, e := s.Read(); e == nil {
				t.Fatal("unsafe directory accepted")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "partial")
	s, e := Open(path, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Read(); e == nil {
		t.Fatal("missing journal accepted")
	}
	s.Close()
}
func TestWrongClaimAndInvalidMutationPreserveBytes(t *testing.T) {
	s, path := createStore(t)
	before, e := os.ReadFile(filepath.Join(path, "journal.json"))
	if e != nil {
		t.Fatal(e)
	}
	j := journal(t, "prepared")
	j["revision"] = json.Number("2")
	j["orgId"] = "another-org"
	if e = s.Save(encoded(t, j)); e == nil {
		t.Fatal("identity rewritten")
	}
	after, e := os.ReadFile(filepath.Join(path, "journal.json"))
	if e != nil || string(before) != string(after) {
		t.Fatal("invalid mutation changed journal")
	}
	wrong := journal(t, "prepared")
	part(wrong, "claim")["sha256"] = string(make([]byte, 64))
	if ValidateClaim(fixture(t, "claim"), wrong) == nil {
		t.Fatal("wrong private claim")
	}
}
