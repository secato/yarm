package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func fixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", "catalog", name))
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestParsePackages(t *testing.T) {
	got, err := ParsePackages(fixture(t, "EffectPackages.ini"))
	if err != nil {
		t.Fatalf("ParsePackages() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d packages, want 3", len(got))
	}

	standard := got[0]
	want := Package{
		ID:                 "standard-effects",
		Name:               "Standard effects",
		Description:        "A small set of utility effects (DisplayDepth, UIMask, ...)",
		InstallPath:        `.\reshade-shaders\Shaders`,
		TextureInstallPath: `.\reshade-shaders\Textures`,
		DownloadURL:        "https://github.com/crosire/reshade-shaders/archive/slim.zip",
		RepositoryURL:      "https://github.com/crosire/reshade-shaders/tree/slim",
		EffectFiles:        []string{"Daltonize.fx", "Deband.fx", "DisplayDepth.fx", "LUT.fx", "UIMask.fx"},
		Enabled:            true,
		Required:           true,
	}
	if diff := cmp.Diff(want, standard); diff != "" {
		t.Errorf("Standard effects mismatch (-want +got):\n%s", diff)
	}

	sweetfx := got[1]
	if sweetfx.ID != "sweetfx-by-ceejay-dk" {
		t.Errorf("SweetFX ID = %q, want %q", sweetfx.ID, "sweetfx-by-ceejay-dk")
	}
	if diff := cmp.Diff([]string{"Template.fx"}, sweetfx.DenyEffectFiles); diff != "" {
		t.Errorf("DenyEffectFiles mismatch (-want +got):\n%s", diff)
	}
	if sweetfx.Required {
		t.Error("SweetFX Required = true, want false (only Standard effects is required)")
	}

	// Legacy has neither Enabled nor Required.
	if got[2].Enabled || got[2].Required {
		t.Errorf("Legacy effects: Enabled=%v Required=%v, want both false", got[2].Enabled, got[2].Required)
	}
}

// A section with no DownloadUrl has nothing to install and must not appear.
func TestParsePackagesSkipsUnusable(t *testing.T) {
	const input = "[00]\nPackageName=No download\nRepositoryUrl=https://example.invalid\n" +
		"[01]\nDownloadUrl=https://example.invalid/a.zip\n" + // no name
		"[02]\nPackageName=Good\nDownloadUrl=https://example.invalid/b.zip\n"

	got, err := ParsePackages(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParsePackages() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "Good" {
		t.Fatalf("got %+v, want only the \"Good\" package", got)
	}
}

// Two packages sharing a name must still get distinct ids, or one would
// silently shadow the other.
func TestParsePackagesDeduplicatesIDs(t *testing.T) {
	const input = "[00]\nPackageName=Same Name\nDownloadUrl=https://example.invalid/a.zip\n" +
		"[01]\nPackageName=Same Name\nDownloadUrl=https://example.invalid/b.zip\n" +
		"[02]\nPackageName=Same Name\nDownloadUrl=https://example.invalid/c.zip\n"

	got, err := ParsePackages(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParsePackages() error = %v", err)
	}
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	want := []string{"same-name", "same-name-2", "same-name-3"}
	if diff := cmp.Diff(want, ids); diff != "" {
		t.Errorf("ids mismatch (-want +got):\n%s", diff)
	}
}
