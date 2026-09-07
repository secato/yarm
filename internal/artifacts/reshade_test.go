package artifacts

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// fakeSetupExe writes a file shaped like a ReShade setup executable: a
// block of PE-ish prefix bytes followed by an appended zip. That is how
// the real installer is built, and shaping one here means the extraction
// path is tested without a network download.
func fakeSetupExe(t *testing.T, path string, files map[string][]byte) {
	t.Helper()

	prefix := make([]byte, 4096)
	if _, err := rand.Read(prefix); err != nil {
		t.Fatalf("rand: %v", err)
	}
	// Make it look like a PE so anything sniffing magic bytes agrees.
	copy(prefix, []byte("MZ"))

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	out := append(append([]byte(nil), prefix...), zipBuf.Bytes()...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestExtractReShade(t *testing.T) {
	dir := t.TempDir()
	setup := filepath.Join(dir, "ReShade_Setup_6.8.0_Addon.exe")

	fakeSetupExe(t, setup, map[string][]byte{
		ReShade32:           []byte("32-bit dll"),
		ReShade64:           []byte("64-bit dll"),
		"ReShade32.json":    []byte("{}"), // ignored
		"ReShade64_XR.json": []byte("{}"), // ignored
	})

	dst := filepath.Join(dir, "out")
	if err := ExtractReShade(setup, dst); err != nil {
		t.Fatalf("ExtractReShade() error = %v", err)
	}

	for name, want := range map[string]string{
		ReShade32: "32-bit dll",
		ReShade64: "64-bit dll",
	} {
		got, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s content = %q, want %q", name, got, want)
		}
	}

	// The json entries must not be extracted.
	for _, name := range []string{"ReShade32.json", "ReShade64_XR.json"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err == nil {
			t.Errorf("%s should not have been extracted", name)
		}
	}
}

// Half a ReShade install is worse than none: a setup exe missing either
// DLL must fail rather than extract what it has.
func TestExtractReShadeRequiresBothDLLs(t *testing.T) {
	tests := []struct {
		name  string
		files map[string][]byte
	}{
		{"missing 64-bit", map[string][]byte{ReShade32: []byte("x")}},
		{"missing 32-bit", map[string][]byte{ReShade64: []byte("x")}},
		{"neither", map[string][]byte{"ReShade32.json": []byte("{}")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			setup := filepath.Join(dir, "setup.exe")
			fakeSetupExe(t, setup, tt.files)

			if err := ExtractReShade(setup, filepath.Join(dir, "out")); err == nil {
				t.Error("want an error, got nil")
			}
		})
	}
}

func TestExtractReShadeRejectsNonArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-an-archive.exe")
	if err := os.WriteFile(path, []byte("MZ definitely not a zip"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := ExtractReShade(path, filepath.Join(dir, "out")); err == nil {
		t.Error("want an error for a file with no appended zip, got nil")
	}
}

func TestDLLFor(t *testing.T) {
	if got := DLLFor(true); got != ReShade64 {
		t.Errorf("DLLFor(true) = %q, want %q", got, ReShade64)
	}
	if got := DLLFor(false); got != ReShade32 {
		t.Errorf("DLLFor(false) = %q, want %q", got, ReShade32)
	}
}

// Reading the appended zip must not be disturbed by the PE prefix, however
// large it is: the central directory's offsets are relative to the
// archive, and the reader derives the base offset itself.
func TestExtractReShadeWithLargePrefix(t *testing.T) {
	dir := t.TempDir()
	setup := filepath.Join(dir, "setup.exe")

	prefix := make([]byte, 1<<20) // 1 MB
	if _, err := rand.Read(prefix); err != nil {
		t.Fatalf("rand: %v", err)
	}
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	for _, name := range []string{ReShade32, ReShade64} {
		w, _ := zw.Create(name)
		_, _ = io.WriteString(w, name+" content")
	}
	_ = zw.Close()

	if err := os.WriteFile(setup, append(prefix, zipBuf.Bytes()...), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := ExtractReShade(setup, filepath.Join(dir, "out")); err != nil {
		t.Errorf("ExtractReShade() with a 1 MB prefix: %v", err)
	}
}
