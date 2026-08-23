//go:build !windows

package handlers

import "golang.org/x/sys/unix"

func filesystemSpace(path string) (total, available uint64, ok bool) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil || stat.Blocks == 0 {
		return 0, 0, false
	}
	return stat.Blocks * uint64(stat.Bsize), stat.Bavail * uint64(stat.Bsize), true
}
