package archive

import (
	"archive/zip"
	"io"
	"strings"
)

// zipEntry adapts *zip.File to Entry.
type zipEntry struct{ f *zip.File }

func (z zipEntry) Name() string { return z.f.Name }
func (z zipEntry) Size() int64  { return int64(z.f.UncompressedSize64) }
func (z zipEntry) IsDir() bool {
	return z.f.FileInfo().IsDir() || strings.HasSuffix(z.f.Name, "/")
}
func (z zipEntry) Open() (io.ReadCloser, error) { return z.f.Open() }

// FromZip adapts a standard library zip reader to this package's Entry
// slice.
func FromZip(zr *zip.Reader) []Entry {
	out := make([]Entry, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, zipEntry{f: f})
	}
	return out
}
