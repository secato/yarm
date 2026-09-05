package fsutil

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "file.txt")

	if err := AtomicWrite(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("AtomicWrite() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected only the final file, got %d entries", len(entries))
	}

	// Overwrite: no leftover temp files, content replaced.
	if err := AtomicWrite(path, []byte("world"), 0o644); err != nil {
		t.Fatalf("AtomicWrite() overwrite error = %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "world" {
		t.Errorf("content after overwrite = %q, want %q", got, "world")
	}
}

func TestCopyHashed(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "out", "dst.bin")
	content := []byte("the quick brown fox jumps over the lazy dog")

	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	sum, size, err := CopyHashed(dst, src)
	if err != nil {
		t.Fatalf("CopyHashed() error = %v", err)
	}

	if size != int64(len(content)) {
		t.Errorf("size = %d, want %d", size, len(content))
	}

	want := sha256.Sum256(content)
	if sum != hex.EncodeToString(want[:]) {
		t.Errorf("sha256 = %s, want %s", sum, hex.EncodeToString(want[:]))
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile(dst) error = %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("dst content = %q, want %q", got, content)
	}
}

func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"a.txt":        "12345",
		"sub/b.txt":    "1234567890",
		"sub/deep/c.x": "abc",
	}

	var want int64
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		want += int64(len(content))
	}

	got, err := DirSize(dir)
	if err != nil {
		t.Fatalf("DirSize() error = %v", err)
	}
	if got != want {
		t.Errorf("DirSize() = %d, want %d", got, want)
	}
}

// A destination whose parent path component is a regular file (not a
// directory) cannot be created into; MkdirAll must fail cleanly rather
// than the temp-file dance leaving anything behind.
func TestAtomicWriteMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := AtomicWrite(filepath.Join(blocker, "child", "file.txt"), []byte("x"), 0o644)
	if err == nil {
		t.Fatal("want an error when a path component is a file, got nil")
	}
}

// A directory with no write permission must fail CreateTemp, and leave
// no partial file behind.
func TestAtomicWriteUnwritableDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := AtomicWrite(filepath.Join(dir, "file.txt"), []byte("x"), 0o644)
	if err == nil {
		t.Fatal("want an error writing into a read-only directory, got nil")
	}
}

// A missing source must fail immediately, before anything is created at
// the destination.
func TestCopyHashedMissingSource(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.bin")

	_, _, err := CopyHashed(dst, filepath.Join(dir, "does-not-exist"))
	if err == nil {
		t.Fatal("want an error for a missing source, got nil")
	}
	if _, statErr := os.Stat(dst); statErr == nil {
		t.Error("the destination should not exist after a failed copy")
	}
}

func TestCopyHashedDestMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	blocker := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, err := CopyHashed(filepath.Join(blocker, "child", "dst.bin"), src)
	if err == nil {
		t.Fatal("want an error when the destination's parent is a file, got nil")
	}
}

// DirSize on a path that does not exist at all must report the error
// rather than silently reporting zero, which would look like an empty
// cache instead of a missing one.
func TestDirSizeMissingRoot(t *testing.T) {
	_, err := DirSize(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("want an error for a missing root, got nil")
	}
}

// A single unreadable subdirectory anywhere in the tree must surface as
// an error, not a silently short count.
func TestDirSizeUnreadableSubdir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "locked")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	if _, err := DirSize(dir); err == nil {
		t.Error("want an error walking into an unreadable subdirectory, got nil")
	}
}
