package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// buildZip returns an in-memory zip with the given path->content entries.
// A path ending in "/" becomes a directory entry.
func buildZip(t *testing.T, files map[string]string) []Entry {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip open: %v", err)
	}
	return FromZip(zr)
}

// The whole point of this package: an archive must never write outside
// its destination directory.
func TestSafePathRejectsEscapes(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")

	escapes := []string{
		"a/b/../c/file.txt", // rejected, not silently rewritten to a/c
		"../evil.txt",
		"../../evil.txt",
		"foo/../../evil.txt",
		`..\evil.txt`,           // Windows-style separator
		`foo\..\..\evil.txt`,    // mixed
		"/etc/passwd",           // absolute
		"//evil.txt",            // UNC-style
		"./../evil.txt",         //
		"foo/../bar/../../evil", //
		`C:\\evil.txt`,          // drive letter, rejected on every OS
		"",                      //
		// ":" anywhere: an NTFS alternate data stream on Windows, and a
		// character state.validRelPath refuses, which would fail the whole
		// install at Save rather than here.
		"Shaders/shader.fx:payload.exe",
		"dir:stream/file.fx",
	}
	for _, name := range escapes {
		got, err := SafePath(dst, name)
		if err == nil {
			t.Errorf("SafePath(%q) = %q, want ErrUnsafePath", name, got)
			continue
		}
		if !errors.Is(err, ErrUnsafePath) {
			t.Errorf("SafePath(%q) error = %v, want ErrUnsafePath", name, err)
		}
	}

	safe := map[string]string{
		"file.txt":         filepath.Join(dst, "file.txt"),
		"dir/file.txt":     filepath.Join(dst, "dir", "file.txt"),
		"./dir/file.txt":   filepath.Join(dst, "dir", "file.txt"),
		`dir\file.txt`:     filepath.Join(dst, "dir", "file.txt"),
		"deep/nested/f.fx": filepath.Join(dst, "deep", "nested", "f.fx"),
	}
	for name, want := range safe {
		got, err := SafePath(dst, name)
		if err != nil {
			t.Errorf("SafePath(%q) unexpected error = %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("SafePath(%q) = %q, want %q", name, got, want)
		}
	}
}

// A drive-qualified absolute path must not escape on Windows.
func TestSafePathWindowsDriveLetter(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive letters are only absolute on Windows")
	}
	if _, err := SafePath(t.TempDir(), `C:\evil.txt`); err == nil {
		t.Error("SafePath with a drive letter: want error, got nil")
	}
}

func TestStripPrefix(t *testing.T) {
	tests := []struct{ name, prefix, want string }{
		{"repo-main/Shaders/A.fx", "repo-main", "Shaders/A.fx"},
		{"repo-main/", "repo-main", ""},
		{"A.fx", "", "A.fx"},
		{`repo-main\Shaders\A.fx`, "repo-main", "Shaders/A.fx"},
	}
	for _, tt := range tests {
		if got := StripPrefix(tt.name, tt.prefix); got != tt.want {
			t.Errorf("StripPrefix(%q, %q) = %q, want %q", tt.name, tt.prefix, got, tt.want)
		}
	}
}

func TestFindDir(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		find     string
		maxDepth int
		want     string
	}{
		{
			name:  "shaders at root",
			files: map[string]string{"Shaders/A.fx": "x"},
			find:  "Shaders", maxDepth: 2, want: "Shaders",
		},
		{
			name:  "shaders one level down",
			files: map[string]string{"repo-main/Shaders/A.fx": "x"},
			find:  "Shaders", maxDepth: 2, want: "repo-main/Shaders",
		},
		{
			name:  "case insensitive",
			files: map[string]string{"repo/shaders/A.fx": "x"},
			find:  "Shaders", maxDepth: 2, want: "repo/shaders",
		},
		{
			name:  "beyond max depth",
			files: map[string]string{"a/b/c/Shaders/A.fx": "x"},
			find:  "Shaders", maxDepth: 2, want: "",
		},
		{
			name:  "absent",
			files: map[string]string{"repo/A.fx": "x"},
			find:  "Textures", maxDepth: 2, want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindDir(buildZip(t, tt.files), tt.find, tt.maxDepth)
			if got != tt.want {
				t.Errorf("FindDir(%q) = %q, want %q", tt.find, got, tt.want)
			}
		})
	}
}

func TestExtractEntry(t *testing.T) {
	entries := buildZip(t, map[string]string{"Shaders/A.fx": "// effect"})
	dst := filepath.Join(t.TempDir(), "out", "A.fx")

	var target Entry
	for _, e := range entries {
		if !e.IsDir() {
			target = e
		}
	}
	if target == nil {
		t.Fatal("no file entry in fixture zip")
	}

	if err := NewBudget(DefaultLimits()).ExtractEntry(target, dst); err != nil {
		t.Fatalf("ExtractEntry() error = %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(got) != "// effect" {
		t.Errorf("extracted content = %q", got)
	}
}

// An end-to-end extraction of a GitHub-shaped package zip.
func TestExtractPackageShape(t *testing.T) {
	entries := buildZip(t, map[string]string{
		"SweetFX-master/Shaders/LumaSharpen.fx": "// luma",
		"SweetFX-master/Shaders/Common.fxh":     "// header",
		"SweetFX-master/Textures/noise.png":     "png",
		"SweetFX-master/README.md":              "readme",
		"SweetFX-master/../escape.txt":          "nope",
	})

	// The same two calls artifacts/packages.go makes: find where the
	// shaders actually live, and strip everything above them.
	top := FindDir(entries, "Shaders", 4)
	if top != "SweetFX-master/Shaders" {
		t.Fatalf("FindDir() = %q", top)
	}
	wrapper := strings.TrimSuffix(top, "/Shaders")

	dst := t.TempDir()
	var extracted []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		rel := StripPrefix(e.Name(), wrapper)
		if rel == "" {
			continue
		}
		full, err := SafePath(dst, rel)
		if err != nil {
			continue // the escape entry is refused
		}
		if err := NewBudget(DefaultLimits()).ExtractEntry(e, full); err != nil {
			t.Fatalf("ExtractEntry(%s): %v", rel, err)
		}
		extracted = append(extracted, rel)
	}

	want := []string{"README.md", "Shaders/Common.fxh", "Shaders/LumaSharpen.fx", "Textures/noise.png"}
	got := append([]string(nil), extracted...)
	sortStrings(got)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("extracted files mismatch (-want +got):\n%s", diff)
	}

	// Nothing may exist outside dst.
	if _, err := os.Stat(filepath.Join(filepath.Dir(dst), "escape.txt")); err == nil {
		t.Error("an archive entry escaped the destination directory")
	}
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if strings.Compare(s[j], s[i]) < 0 {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}
