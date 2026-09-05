//go:build windows

package fsutil

import (
	"io/fs"

	"golang.org/x/sys/windows"
)

// FreeSpace returns the number of bytes free on the filesystem holding
// path, via GetDiskFreeSpaceEx.
func FreeSpace(path string) (int64, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, &fs.PathError{Op: "statfs", Path: path, Err: err}
	}

	var freeAvail, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &freeAvail, &total, &totalFree); err != nil {
		return 0, &fs.PathError{Op: "statfs", Path: path, Err: err}
	}
	return int64(freeAvail), nil
}
