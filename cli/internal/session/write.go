//go:build darwin || linux

package session

import (
	"crypto/rand"
	"encoding/hex"
	"golang.org/x/sys/unix"
)

// Create persists both files before returning an actionable session.
func Create(path string, journal, claim []byte) (*Store, error) {
	j, e := ParseJournal(journal)
	if e != nil || number(j["revision"]) != 1 || part(j, "admission")["state"] != "prepared" || part(j, "claim")["state"] != "not_started" || part(j, "execution")["state"] != "not_started" || part(j, "capture")["state"] != "not_started" || part(j, "finish")["state"] != "not_started" || part(j, "cancellation")["state"] != "not_requested" || j["observation"] != nil || j["recovery"] != nil {
		return nil, ErrCorrupt
	}
	if e = ValidateClaim(claim, j); e != nil {
		return nil, e
	}
	s, e := Open(path, true)
	if e != nil {
		return nil, e
	}
	if e = s.writeNew("claim.json", claim); e == nil {
		e = s.writeNew("journal.json", journal)
	}
	if e != nil {
		s.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) writeNew(name string, b []byte) error {
	if e := s.check(); e != nil {
		return e
	}
	f, e := s.openFile(name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = s.syncFile(f)
	}
	closeError := f.Close()
	if e == nil {
		e = closeError
	}
	if e == nil {
		e = s.syncFile(s.directory)
	}
	if e == nil {
		e = s.check()
	}
	if e != nil {
		s.failed = true
		return failure("session_write_failed")
	}
	return nil
}

// Save accepts only a complete next revision. An ambiguous local write poisons
// this handle: reopen and validate it before any subsequent dispatch.
func (s *Store) Save(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, e := s.readJournal()
	if e != nil {
		return e
	}
	next, e := ParseJournal(data)
	if e != nil || !Transition(old, next) {
		return ErrCorrupt
	}
	token := make([]byte, 16)
	if _, e = rand.Read(token); e != nil {
		return failure("session_write_failed")
	}
	name := ".pending-" + hex.EncodeToString(token)
	f, e := s.openFile(name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if e != nil {
		return e
	}
	defer unix.Unlinkat(int(s.directory.Fd()), name, 0)
	_, e = f.Write(data)
	if e == nil {
		e = s.syncFile(f)
	}
	closeError := f.Close()
	if e == nil {
		e = closeError
	}
	if e == nil {
		e = s.check()
	}
	if e == nil {
		e = unix.Renameat(int(s.directory.Fd()), name, int(s.directory.Fd()), "journal.json")
	}
	if e == nil {
		e = s.syncFile(s.directory)
	}
	if e == nil {
		e = s.check()
	}
	if e == nil {
		e = s.check()
	}
	if e != nil {
		s.failed = true
		return failure("session_write_failed")
	}
	return nil
}
