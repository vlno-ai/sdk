//go:build darwin

package session

import "golang.org/x/sys/unix"

func supportedFilesystem(fd int) bool {
	var info unix.Statfs_t
	if unix.Fstatfs(fd, &info) != nil {
		return false
	}
	name := []byte{}
	for _, c := range info.Fstypename {
		if c == 0 {
			break
		}
		name = append(name, byte(c))
	}
	return string(name) == "apfs" && info.Flags&unix.MNT_LOCAL != 0
}
