package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func mkfile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestScanCustom(t *testing.T) {
	root := t.TempDir()

	// A shader pack in the documented Shaders/ layout.
	mkfile(t, filepath.Join(root, "shaders", "My Pack", "Shaders", "Cool.fx"), "// fx")
	mkfile(t, filepath.Join(root, "shaders", "My Pack", "Textures", "t.png"), "png")

	// A shader pack with loose .fx files at the top level.
	mkfile(t, filepath.Join(root, "shaders", "Loose", "Thing.fx"), "// fx")

	// A shader folder with a display-name override.
	mkfile(t, filepath.Join(root, "shaders", "raw-name", "Shaders", "A.fx"), "// fx")
	mkfile(t, filepath.Join(root, "shaders", "raw-name", customMetaFile),
		`{"name":"Pretty Name","description":"From yarm.json"}`)

	// An addon folder.
	mkfile(t, filepath.Join(root, "addons", "MyAddon", "thing.addon64"), "bin")

	// Neither of these holds installable content.
	mkfile(t, filepath.Join(root, "shaders", "Empty", "readme.txt"), "nothing here")
	mkfile(t, filepath.Join(root, "addons", "NoBinary", "readme.txt"), "nothing here")

	got, err := ScanCustom(root)
	if err != nil {
		t.Fatalf("ScanCustom() error = %v", err)
	}

	var names []string
	for _, c := range got {
		names = append(names, c.Name)
	}
	want := []string{"MyAddon", "Loose", "My Pack", "Pretty Name"}
	if diff := cmp.Diff(want, names); diff != "" {
		t.Errorf("custom content mismatch (-want +got):\n%s", diff)
	}

	byID := make(map[string]Custom, len(got))
	for _, c := range got {
		byID[c.ID] = c
	}

	addon, ok := byID["custom:addons:myaddon"]
	if !ok {
		t.Fatal("addon id custom:addons:myaddon not found")
	}
	if addon.Kind != CustomAddons {
		t.Errorf("Kind = %q, want %q", addon.Kind, CustomAddons)
	}

	overridden, ok := byID["custom:shaders:raw-name"]
	if !ok {
		t.Fatal("custom:shaders:raw-name not found")
	}
	if overridden.Name != "Pretty Name" {
		t.Errorf("Name = %q, want %q (yarm.json should win)", overridden.Name, "Pretty Name")
	}
	if overridden.Description != "From yarm.json" {
		t.Errorf("Description = %q", overridden.Description)
	}
}

// Most users never create cache/custom; that is not an error.
func TestScanCustomMissingRoot(t *testing.T) {
	got, err := ScanCustom(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("ScanCustom() on a missing root: error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries, want 0", len(got))
	}
}

// A malformed yarm.json must not hide otherwise valid content.
func TestScanCustomBadMeta(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "shaders", "Pack", "Shaders", "A.fx"), "// fx")
	mkfile(t, filepath.Join(root, "shaders", "Pack", customMetaFile), "{not json")

	got, err := ScanCustom(root)
	if err != nil {
		t.Fatalf("ScanCustom() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "Pack" {
		t.Fatalf("got %+v, want the folder name as a fallback", got)
	}
}
