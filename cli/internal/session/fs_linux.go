//go:build linux

package session

import (
	"golang.org/x/sys/unix"
	"io"
	"os"
	"strconv"
	"strings"
)

func supportedFilesystem(fd int) bool {
	var info unix.Statfs_t
	if unix.Fstatfs(fd, &info) != nil || info.Type != unix.EXT4_SUPER_MAGIC {
		return false
	}
	metadata, ok := procRead("/proc/self/fdinfo/"+strconv.Itoa(fd), 4096)
	if !ok {
		return false
	}
	id := ""
	for _, line := range strings.Split(metadata, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "mnt_id:" {
			if id != "" {
				return false
			}
			id = f[1]
		}
	}
	if _, e := strconv.ParseUint(id, 10, 64); e != nil {
		return false
	}
	mounts, ok := procRead("/proc/self/mountinfo", 1024*1024)
	if !ok {
		return false
	}
	for _, line := range strings.Split(mounts, "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		left, right := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(left) > 0 && left[0] == id {
			return len(right) >= 1 && right[0] == "ext4"
		}
	}
	return false
}
func procRead(path string, limit int) (string, bool) {
	f, e := os.Open(path)
	if e != nil {
		return "", false
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, int64(limit+1)))
	return string(b), e == nil && len(b) <= limit
}
