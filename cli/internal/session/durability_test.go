//go:build darwin || linux

package session

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"testing"
)

func TestFsyncFailureStopsHandleAndRecoveryReadsActualRevision(t *testing.T) {
	for _, cut := range []int{1, 2} {
		t.Run(string(rune('0'+cut)), func(t *testing.T) {
			s, path := createStore(t)
			j, e := s.Read()
			if e != nil {
				t.Fatal(e)
			}
			nextRevision(j)
			part(j, "admission")["state"] = "unknown"
			calls := 0
			s.syncFile = func(f *os.File) error {
				calls++
				if calls == cut {
					return errors.New("synthetic fsync failure")
				}
				return f.Sync()
			}
			if e = s.Save(encoded(t, j)); e == nil {
				t.Fatal("fsync failure ignored")
			}
			if _, e = s.Read(); e == nil {
				t.Fatal("ambiguous handle still usable")
			}
			if e = s.Save(encoded(t, j)); e == nil {
				t.Fatal("second dispatch could continue")
			}
			s.Close()
			reopened, e := Open(path, false)
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			got, e := reopened.Read()
			if e != nil {
				t.Fatal(e)
			}
			want := int64(1)
			if cut == 2 {
				want = 2
			}
			if number(got["revision"]) != want {
				t.Fatalf("got revision %v, want %v", got["revision"], want)
			}
		})
	}
}
func TestLockAndDirectoryAreCloseOnExec(t *testing.T) {
	s, _ := createStore(t)
	for _, file := range []*os.File{s.lock, s.directory} {
		flags, e := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
		if e != nil || flags&unix.FD_CLOEXEC == 0 {
			t.Fatal("descriptor inherited across exec", e)
		}
	}
}
