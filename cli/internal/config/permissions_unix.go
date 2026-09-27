//go:build !windows

package config

import (
	"errors"
	"os"
	"syscall"
)

func privateFile(info os.FileInfo) error {
	if info.Mode().Perm()&0177 != 0 {
		return errors.New("configuration permissions must allow owner access only (chmod 600)")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(stat.Uid) != uint64(os.Getuid()) {
		return errors.New("configuration must belong to the current user")
	}
	return nil
}
