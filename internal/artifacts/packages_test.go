package artifacts

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func writeZip(t *testing.T, path string, files map[string]string) {
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
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// listFiles returns every file under root, relative and slash-separated.
func listFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func TestNormalizePackageGitHubLayout(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pkg.zip")

	// The shape a GitHub branch archive actually has.
	writeZip(t, zipPath, map[string]string{
		"SweetFX-master/Shaders/LumaSharpen.fx": "// luma",
		"SweetFX-master/Shaders/Common.fxh":     "// header",
		"SweetFX-master/Textures/noise.png":     "png-bytes",
		"SweetFX-master/README.md":              "readme",
		"SweetFX-master/.gitignore":             "ignored",
	})

	dst := filepath.Join(dir, "out")
	meta := PackageMeta{
		ID:              "sweetfx-by-ceejay-dk",
		Name:            "SweetFX by CeeJay.dk",
		EffectFiles:     []string{"LumaSharpen.fx"},
		DenyEffectFiles: []string{"Template.fx"},
		SourceURL:       "https://example.invalid/sweetfx.zip",
	}
	if err := NormalizePackage(zipPath, dst, meta); err != nil {
		t.Fatalf("NormalizePackage() error = %v", err)
	}

	want := []string{
		"Shaders/Common.fxh",
		"Shaders/LumaSharpen.fx",
		"Textures/noise.png",
		MetaFile,
	}
	got := listFiles(t, dst)
	sortStrings(got)
	sortStrings(want)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("normalized layout mismatch (-want +got):\n%s", diff)
		t.Log("the repository wrapper dir must be stripped, and non-shader files dropped")
	}

	raw, err := os.ReadFile(filepath.Join(dst, MetaFile))
	if err != nil {
		t.Fatalf("read %s: %v", MetaFile, err)
	}
	var gotMeta PackageMeta
	if err := json.Unmarshal(raw, &gotMeta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if diff := cmp.Diff(meta, gotMeta); diff != "" {
		t.Errorf("package.json mismatch (-want +got):\n%s", diff)
	}
}

// Some repositories publish .fx files at the top level with no Shaders/
// directory; they must normalize to the same shape.
func TestNormalizePackageFlatLayout(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pkg.zip")
	writeZip(t, zipPath, map[string]string{
		"repo-main/Effect.fx":  "// fx",
		"repo-main/Header.fxh": "// fxh",
		"repo-main/README.md":  "readme",
	})

	dst := filepath.Join(dir, "out")
	if err := NormalizePackage(zipPath, dst, PackageMeta{ID: "x", Name: "X"}); err != nil {
		t.Fatalf("NormalizePackage() error = %v", err)
	}

	want := []string{MetaFile, "Shaders/Effect.fx", "Shaders/Header.fxh"}
	got := listFiles(t, dst)
	sortStrings(got)
	sortStrings(want)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("flat layout mismatch (-want +got):\n%s", diff)
	}
}

// An archive whose entries try to traverse must not write outside dst.
func TestNormalizePackageRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pkg.zip")
	writeZip(t, zipPath, map[string]string{
		"Shaders/Good.fx":    "// good",
		"Shaders/../../x.fx": "// escape",
	})

	dst := filepath.Join(dir, "out")
	if err := NormalizePackage(zipPath, dst, PackageMeta{ID: "x", Name: "X"}); err != nil {
		t.Fatalf("NormalizePackage() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "x.fx")); err == nil {
		t.Error("a traversing entry escaped the destination")
	}
	if _, err := os.Stat(filepath.Join(dst, "Shaders", "Good.fx")); err != nil {
		t.Errorf("the safe entry should still be extracted: %v", err)
	}
}

// A zip with nothing shader-shaped in it is an error, not an empty cache
// entry that later looks installable.
func TestNormalizePackageEmpty(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pkg.zip")
	writeZip(t, zipPath, map[string]string{"repo-main/README.md": "nothing here"})

	if err := NormalizePackage(zipPath, filepath.Join(dir, "out"), PackageMeta{ID: "x"}); err == nil {
		t.Error("want an error for a package with no shaders, got nil")
	}
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}
