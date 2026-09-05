//go:build linux || darwin

package fsutil

import (
	"io/fs"

	"golang.org/x/sys/unix"
)

// FreeSpace returns the number of bytes free on the filesystem holding
// path, via statfs(2).
func FreeSpace(path string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, &fs.PathError{Op: "statfs", Path: path, Err: err}
	}
	return int64(st.Bsize) * int64(st.Bavail), nil
}
