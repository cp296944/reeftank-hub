//go:build linux

package main

import "syscall"

// diskKB returns the total and available space (KiB) of the filesystem holding
// `path`.
func diskKB(path string) (total, free int) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bs := int64(st.Bsize)
	return int(int64(st.Blocks) * bs / 1024), int(int64(st.Bavail) * bs / 1024)
}
