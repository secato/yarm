package game

import (
	"os"
	"path/filepath"
	"testing"
)

const fixtureDir = "../../testdata/pe"

func TestInspect(t *testing.T) {
	tests := []struct {
		file     string
		wantArch Arch
		wantAPI  API
	}{
		{"x64_dxgi.bin", ArchX64, APIDXGI},
		{"x64_d3d11.bin", ArchX64, APID3D11}, // imports both d3d11.dll and dxgi.dll; d3d11 takes priority
		{"x64_d3d12.bin", ArchX64, APID3D12},
		{"x86_d3d10.bin", ArchX86, APID3D10}, // "d3d10_1.dll" matches the d3d10* prefix
		{"x86_d3d9.bin", ArchX86, APID3D9},
		{"x86_d3d8.bin", ArchX86, APID3D8},
		{"x86_opengl.bin", ArchX86, APIOpenGL},
		{"x64_vulkan.bin", ArchX64, APIVulkan},
		{"x64_unknown.bin", ArchX64, APIUnknown},    // imports only kernel32.dll
		{"x64_no_imports.bin", ArchX64, APIUnknown}, // no import directory at all
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			arch, api := Inspect(filepath.Join(fixtureDir, tt.file))
			if arch != tt.wantArch {
				t.Errorf("Inspect(%s) arch = %s, want %s", tt.file, arch, tt.wantArch)
			}
			if api != tt.wantAPI {
				t.Errorf("Inspect(%s) api = %s, want %s", tt.file, api, tt.wantAPI)
			}
		})
	}
}

func TestAPISupported(t *testing.T) {
	unsupported := map[API]bool{APID3D8: true, APIVulkan: true}
	for _, api := range []API{
		APIUnknown, APID3D8, APID3D9, APID3D10, APID3D11, APID3D12, APIDXGI, APIOpenGL, APIVulkan,
	} {
		want := !unsupported[api]
		if got := api.Supported(); got != want {
			t.Errorf("%s.Supported() = %v, want %v", api, got, want)
		}
	}
}

func TestAPIRecommendedDLL(t *testing.T) {
	tests := map[API]string{
		APID3D9:    "d3d9.dll",
		APID3D10:   "dxgi.dll",
		APID3D11:   "dxgi.dll",
		APID3D12:   "dxgi.dll",
		APIDXGI:    "dxgi.dll",
		APIOpenGL:  "opengl32.dll",
		APID3D8:    "",
		APIVulkan:  "",
		APIUnknown: "",
	}
	for api, want := range tests {
		if got := api.RecommendedDLL(); got != want {
			t.Errorf("%s.RecommendedDLL() = %q, want %q", api, got, want)
		}
	}
}

func TestInspectNonPEFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-pe.exe")
	if err := os.WriteFile(path, []byte("not a pe file"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	arch, api := Inspect(path)
	if arch != ArchUnknown || api != APIUnknown {
		t.Errorf("Inspect(garbage) = (%s, %s), want (%s, %s)", arch, api, ArchUnknown, APIUnknown)
	}
}

func TestInspectMissingFile(t *testing.T) {
	arch, api := Inspect(filepath.Join(t.TempDir(), "does-not-exist.exe"))
	if arch != ArchUnknown || api != APIUnknown {
		t.Errorf("Inspect(missing) = (%s, %s), want (%s, %s)", arch, api, ArchUnknown, APIUnknown)
	}
}

// TestInspectMalformedImportDirectory guards against a real crash: an
// import descriptor pointing outside its section panicked
// (*pe.File).ImportedSymbols with a slice-bounds-out-of-range on Go
// toolchains up to 1.25.x (fixed in 1.26, but debug/pe's own doc still
// warns malformed input "may ... cause panics"). Inspect must survive any
// such executable rather than take the whole games scan down with it.
func TestInspectMalformedImportDirectory(t *testing.T) {
	arch, api := Inspect(filepath.Join(fixtureDir, "x64_malformed_import.bin"))
	if arch != ArchX64 || api != APIUnknown {
		t.Errorf("Inspect(malformed) = (%s, %s), want (%s, %s)", arch, api, ArchX64, APIUnknown)
	}
}
