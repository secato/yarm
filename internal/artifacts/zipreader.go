package artifacts

import (
	"archive/zip"
	"io"
)

// zipReader opens a zip over a ReaderAt, wrapping the standard library so
// the artifact code reads the same way for plain zips and for the
// zip-appended setup executable.
func zipReader(r io.ReaderAt, size int64) (*zip.Reader, error) {
	return zip.NewReader(r, size)
}
