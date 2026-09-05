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
