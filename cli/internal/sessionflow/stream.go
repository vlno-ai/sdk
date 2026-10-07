package sessionflow

import (
	"context"
	"strings"
	"sync"
	"unicode/utf8"
)

type capture struct {
	mu      sync.Mutex
	p       *Protocol
	ctx     context.Context
	stop    context.CancelFunc
	secrets []string
	err     error
}
type stream struct {
	capture *capture
	kind    string
	pending []byte
}

func (s *stream) Write(b []byte) (int, error) {
	c := s.capture
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return 0, c.err
	}
	s.pending = append(s.pending, b...)
	if e := s.drain(false); e != nil {
		c.err = e
		c.stop()
		return 0, e
	}
	return len(b), nil
}
func (s *stream) drain(final bool) error {
	for len(s.pending) > 0 {
		cut := 0
		if newline := strings.IndexByte(string(s.pending), '\n'); newline >= 0 {
			cut = newline + 1
		} else if final {
			cut = len(s.pending)
		} else if len(s.pending) > 4096 {
			cut = len(s.pending) - 2048
		} else {
			return nil
		}
		if cut > 2048 {
			cut = 2048
		}
		for cut > 0 && cut < len(s.pending) && !utf8.RuneStart(s.pending[cut]) {
			cut--
		}
		if cut == 0 {
			if !final && len(s.pending) < 4 {
				return nil
			}
			cut = 1
		}
		for _, secret := range s.capture.secrets {
			if secret == "" {
				continue
			}
			for offset := 0; offset < len(s.pending); {
				i := strings.Index(string(s.pending[offset:]), secret)
				if i < 0 {
					break
				}
				i += offset
				if i < cut && i+len(secret) > cut {
					cut = i
				}
				offset = i + len(secret)
			}
		}
		if cut == 0 {
			if !final && len(s.pending) < 4096 {
				return nil
			}
			cut = len(s.pending)
			if cut > 4096 {
				cut = 4096
			}
		}
		data := strings.ToValidUTF8(string(s.pending[:cut]), "\ufffd")
		if e := s.capture.p.Record(s.kind, map[string]any{"text": data}, s.capture.secrets, false); e != nil {
			return e
		}
		s.pending = s.pending[cut:]
		if e := s.capture.p.Flush(s.capture.ctx); e != nil {
			return e
		}
	}
	return nil
}
func (c *capture) finish(streams ...*stream) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	for _, s := range streams {
		if e := s.drain(true); e != nil {
			c.err = e
			return e
		}
	}
	return nil
}
func (c *capture) failure() error { c.mu.Lock(); defer c.mu.Unlock(); return c.err }
func isFilesystemFailure(e error) bool {
	return e != nil && (strings.HasPrefix(e.Error(), "session_file") || e.Error() == "session_closed" || e.Error() == "session_corrupt" ||
		e.Error() == "session_write_failed" ||
		e.Error() == "session_path_changed")
}
