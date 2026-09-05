package artifacts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/secato/yarm/internal/game"
)

func TestExtractAddonZipPicksByArch(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "addon.zip")
	writeZip(t, zipPath, map[string]string{
		"release/thing.addon32": "32-bit",
		"release/thing.addon64": "64-bit",
		"release/README.md":     "readme",
	})

	tests := []struct {
		arch     game.Arch
		wantFile string
		wantBody string
	}{
		{game.ArchX64, "thing.addon64", "64-bit"},
		{game.ArchX86, "thing.addon32", "32-bit"},
		{game.ArchUnknown, "thing.addon64", "64-bit"}, // defaults to 64-bit
	}

	for _, tt := range tests {
		t.Run(string(tt.arch), func(t *testing.T) {
			dst := filepath.Join(dir, "out-"+string(tt.arch))
			written, err := ExtractAddonZip(zipPath, dst, tt.arch)
			if err != nil {
				t.Fatalf("ExtractAddonZip() error = %v", err)
			}
			if len(written) != 1 {
				t.Fatalf("wrote %d files, want 1", len(written))
			}

			// Directory structure inside the zip is discarded: ReShade
			// loads add-ons from the directory holding its DLL.
			got, err := os.ReadFile(filepath.Join(dst, tt.wantFile))
			if err != nil {
				t.Fatalf("read %s: %v", tt.wantFile, err)
			}
			if string(got) != tt.wantBody {
				t.Errorf("content = %q, want %q", got, tt.wantBody)
			}
			if _, err := os.Stat(filepath.Join(dst, "README.md")); err == nil {
				t.Error("non-addon files should not be extracted")
			}
		})
	}
}

// An archive with no add-on files at all means the catalog promised
// something it does not deliver.
func TestExtractAddonZipWithoutAddons(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "addon.zip")
	writeZip(t, zipPath, map[string]string{"release/some.dll": "not an addon"})

	if _, err := ExtractAddonZip(zipPath, filepath.Join(dir, "out"), game.ArchX64); err == nil {
		t.Error("want an error for a zip with no .addon32/.addon64, got nil")
	}
}

// A 64-bit-only archive has nothing for a 32-bit game.
func TestExtractAddonZipMissingArch(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "addon.zip")
	writeZip(t, zipPath, map[string]string{"thing.addon64": "64-bit"})

	if _, err := ExtractAddonZip(zipPath, filepath.Join(dir, "out"), game.ArchX86); err == nil {
		t.Error("want an error when the requested arch is absent, got nil")
	}
}

func TestInstallAddonFile(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name     string
		src      string
		arch     game.Arch
		wantName string
	}{
		{"already suffixed", "swapchain_override.addon64", game.ArchX64, "swapchain_override.addon64"},
		{"already suffixed 32", "swapchain_override.addon32", game.ArchX86, "swapchain_override.addon32"},
		// One catalog entry publishes a bare ".addon"; ReShade only scans
		// for .addon32/.addon64, so it must be renamed.
		{"bare .addon on x64", "frame_capture.addon", game.ArchX64, "frame_capture.addon64"},
		{"bare .addon on x86", "frame_capture.addon", game.ArchX86, "frame_capture.addon32"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := filepath.Join(dir, tt.src)
			if err := os.WriteFile(src, []byte("addon binary"), 0o644); err != nil {
				t.Fatalf("write src: %v", err)
			}

			dst := filepath.Join(dir, "out-"+tt.name)
			got, err := InstallAddonFile(src, dst, tt.arch)
			if err != nil {
				t.Fatalf("InstallAddonFile() error = %v", err)
			}
			if filepath.Base(got) != tt.wantName {
				t.Errorf("installed as %q, want %q", filepath.Base(got), tt.wantName)
			}
			if _, err := os.Stat(got); err != nil {
				t.Errorf("installed file missing: %v", err)
			}
		})
	}
}
