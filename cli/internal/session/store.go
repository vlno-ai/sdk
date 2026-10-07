//go:build darwin || linux

package session

import (
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Store owns a stable flock. It does not dispatch APIs or execute processes.
type Store struct {
	mu              sync.Mutex
	path            string
	directory, lock *os.File
	failed          bool
	syncFile        func(*os.File) error
}

func failure(code string) error { return errors.New(code) }
func private(f *os.File) bool {
	var s unix.Stat_t
	return unix.Fstat(int(f.Fd()), &s) == nil && s.Mode&unix.S_IFMT == unix.S_IFREG && s.Mode&07777 == 0600 && s.Uid == uint32(os.Geteuid()) && s.Nlink == 1
}

// Open acquires the directory lock; create rejects any existing directory.
func Open(path string, create bool) (s *Store, err error) {
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, failure("session_directory_invalid")
	}
	if create {
		if os.Mkdir(path, 0700) != nil {
			return nil, failure("session_directory_exists")
		}
		parent, e := os.Open(filepath.Dir(path))
		if e != nil {
			return nil, failure("session_write_failed")
		}
		e = parent.Sync()
		parent.Close()
		if e != nil {
			return nil, failure("session_write_failed")
		}
	}
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, failure("session_directory_invalid")
	}
	s = &Store{path: path, directory: os.NewFile(uintptr(fd), path), syncFile: func(f *os.File) error { return f.Sync() }}
	defer func() {
		if err != nil {
			s.Close()
			s = nil
		}
	}()
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Uid != uint32(os.Geteuid()) || info.Mode&07777 != 0700 {
		return s, failure("session_directory_not_private")
	}
	if !supportedFilesystem(fd) {
		return s, failure("session_filesystem_unsupported")
	}
	s.lock, e = s.openFile("lock", unix.O_RDWR|unix.O_CREAT)
	if e != nil {
		return s, e
	}
	if e = unix.Flock(int(s.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return s, failure("session_in_use")
	}
	if e = s.check(); e != nil {
		return s, e
	}
	return s, nil
}
func (s *Store) openFile(name string, flags int) (*os.File, error) {
	fd, e := unix.Openat(int(s.directory.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if e != nil {
		if e == unix.ENOENT && name == "journal.json" {
			return nil, failure("session_incomplete")
		}
		if e == unix.ENOENT && name == "claim.json" {
			return nil, ErrCorrupt
		}
		return nil, failure("session_file_invalid")
	}
	f := os.NewFile(uintptr(fd), name)
	if !private(f) {
		f.Close()
		return nil, failure("session_file_not_private")
	}
	return f, nil
}
func (s *Store) check() error {
	if s == nil || s.directory == nil || s.lock == nil || s.failed {
		return failure("session_closed")
	}
	var current, opened, lock unix.Stat_t
	if unix.Lstat(s.path, &current) != nil || unix.Fstat(int(s.directory.Fd()), &opened) != nil || current.Dev != opened.Dev || current.Ino != opened.Ino || current.Mode&unix.S_IFMT != unix.S_IFDIR || current.Mode&07777 != 0700 || current.Uid != uint32(os.Geteuid()) {
		return failure("session_path_changed")
	}
	if unix.Fstatat(int(s.directory.Fd()), "lock", &current, unix.AT_SYMLINK_NOFOLLOW) != nil || unix.Fstat(int(s.lock.Fd()), &lock) != nil || current.Dev != lock.Dev || current.Ino != lock.Ino || !private(s.lock) {
		return failure("session_path_changed")
	}
	return nil
}
func (s *Store) read(name string, limit int) ([]byte, error) {
	if e := s.check(); e != nil {
		return nil, e
	}
	f, e := s.openFile(name, unix.O_RDONLY)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, int64(limit+1)))
	if e != nil || len(b) > limit {
		return nil, ErrCorrupt
	}
	return b, nil
}

// Read returns a newly decoded validated journal and verifies its private claim.
func (s *Store) Read() (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readJournal()
}
func (s *Store) readJournal() (map[string]any, error) {
	b, e := s.read("journal.json", MaximumJournal)
	if e != nil {
		return nil, e
	}
	j, e := ParseJournal(b)
	if e != nil {
		return nil, e
	}
	c, e := s.read("claim.json", 1024)
	if e != nil {
		return nil, e
	}
	if e = ValidateClaim(c, j); e != nil {
		return nil, e
	}
	return j, nil
}
func (s *Store) Close() error {
	var err error
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock != nil {
		err = s.lock.Close()
		s.lock = nil
	}
	if s.directory != nil {
		e := s.directory.Close()
		s.directory = nil
		if err == nil {
			err = e
		}
	}
	return err
}
