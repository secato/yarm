package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// bombZip builds an archive of highly compressible entries: `count`
// entries of `size` zero bytes each. Zeros compress to almost nothing, so
// a small file on disk expands enormously — the shape of a real zip bomb.
func bombZip(t *testing.T, count int, size int64) []Entry {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zeros := make([]byte, 64*1024)
	for i := range count {
		w, err := zw.Create(fmt.Sprintf("bomb-%03d.fx", i))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		for written := int64(0); written < size; written += int64(len(zeros)) {
			chunk := zeros
			if rem := size - written; rem < int64(len(chunk)) {
				chunk = chunk[:rem]
			}
			if _, err := w.Write(chunk); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	t.Logf("bomb: %d entries x %d bytes = %d bytes expanded, %d bytes on disk",
		count, size, int64(count)*size, buf.Len())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return FromZip(zr)
}

// A per-entry cap alone still permits N x MaxEntrySize. The total budget
// is what actually bounds an archive.
func TestBudgetStopsTotalExpansion(t *testing.T) {
	const (
		entrySize = 256 * 1024
		entries   = 40
	)
	limits := Limits{
		MaxEntrySize:  entrySize * 2, // each entry is individually fine
		MaxTotalSize:  entrySize * 10,
		MaxEntryCount: 1000,
	}

	all := bombZip(t, entries, entrySize)
	dst := t.TempDir()
	budget := NewBudget(limits)

	var extracted int
	var lastErr error
	for i, e := range all {
		if e.IsDir() {
			continue
		}
		err := budget.ExtractEntry(e, filepath.Join(dst, fmt.Sprintf("out-%03d.bin", i)))
		if err != nil {
			lastErr = err
			break
		}
		extracted++
	}

	if lastErr == nil {
		t.Fatal("the whole bomb was extracted; want the budget to stop it")
	}
	if !errors.Is(lastErr, ErrTooLarge) {
		t.Errorf("error = %v, want ErrTooLarge", lastErr)
	}
	// Asserted against what actually landed on disk rather than against
	// the budget's own counter: the counter agreeing with its own limit
	// proves very little, while the bytes written are the thing the limit
	// exists to bound.
	written := dirSize(t, dst)
	if written > limits.MaxTotalSize {
		t.Errorf("extracted %d bytes, over the %d budget", written, limits.MaxTotalSize)
	}
	t.Logf("stopped after %d/%d entries, %d bytes on disk", extracted, entries, written)
}

// Precheck should refuse an obvious bomb without extracting anything.
func TestPrecheckRejectsBombs(t *testing.T) {
	all := bombZip(t, 20, 256*1024)

	tests := []struct {
		name   string
		limits Limits
	}{
		{"total too large", Limits{MaxEntrySize: 1 << 30, MaxTotalSize: 1024, MaxEntryCount: 1000}},
		{"entry too large", Limits{MaxEntrySize: 1024, MaxTotalSize: 1 << 30, MaxEntryCount: 1000}},
		{"too many entries", Limits{MaxEntrySize: 1 << 30, MaxTotalSize: 1 << 30, MaxEntryCount: 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Precheck(all, tt.limits)
			if !errors.Is(err, ErrTooLarge) {
				t.Errorf("Precheck() = %v, want ErrTooLarge", err)
			}
		})
	}

	if err := Precheck(all, DefaultLimits()); err != nil {
		t.Errorf("Precheck() with default limits rejected a benign archive: %v", err)
	}
}

func TestBudgetEntryCount(t *testing.T) {
	all := bombZip(t, 10, 64)
	budget := NewBudget(Limits{MaxEntrySize: 1 << 20, MaxTotalSize: 1 << 20, MaxEntryCount: 3})
	dst := t.TempDir()

	var err error
	for i, e := range all {
		if err = budget.ExtractEntry(e, filepath.Join(dst, fmt.Sprintf("f%d", i))); err != nil {
			break
		}
	}
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("error = %v, want ErrTooLarge once past the entry count", err)
	}
}

// An archive can carry a symlink entry. Honoring one would let an archive
// plant a link that a later entry writes through, escaping the destination
// despite SafePath. Every entry must land as a regular file.
func TestExtractEntryNeverCreatesSymlinks(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	hdr := &zip.FileHeader{Name: "link"}
	hdr.SetMode(os.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatalf("CreateHeader: %v", err)
	}
	if _, err := io.WriteString(w, "/etc/passwd"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "link")
	if err := NewBudget(DefaultLimits()).ExtractEntry(FromZip(zr)[0], dst); err != nil {
		t.Fatalf("ExtractEntry: %v", err)
	}

	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("a symlink entry was extracted as a symlink")
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "/etc/passwd" {
		t.Errorf("content = %q, want the link target stored as plain text", body)
	}
}

// dirSize totals the regular files under dir.
func dirSize(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return total
}
