//go:build !darwin && !linux

package session

import "errors"

type Store struct{}

var unsupported = errors.New("session_platform_unsupported")

func Open(string, bool) (*Store, error)             { return nil, unsupported }
func Create(string, []byte, []byte) (*Store, error) { return nil, unsupported }
func (*Store) Read() (map[string]any, error)        { return nil, unsupported }
func (*Store) Save([]byte) error                    { return unsupported }
func (*Store) Close() error                         { return nil }
