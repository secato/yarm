package catalog

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/secato/yarm/internal/game"
)

func TestParseAddons(t *testing.T) {
	got, err := ParseAddons(fixture(t, "Addons.ini"))
	if err != nil {
		t.Fatalf("ParseAddons() error = %v", err)
	}

	names := make([]string, len(got))
	for i, a := range got {
		names[i] = a.Name
	}
	want := []string{
		"Swap chain override by crosire",
		"ShaderToggler by Otis_Inf",
		"IGCS Connector by Otis_Inf",
		"Frame Capture by murchalloo",
		"ReshadeEffectShaderToggler (REST) by 4lex4nder",
		"RenoDX by ShortFuse",
	}
	if diff := cmp.Diff(want, names); diff != "" {
		t.Errorf("addon names mismatch (-want +got):\n%s", diff)
		t.Log("a commented-out block must never appear as an addon")
	}
}

func TestAddonClassification(t *testing.T) {
	addons, err := ParseAddons(fixture(t, "Addons.ini"))
	if err != nil {
		t.Fatalf("ParseAddons() error = %v", err)
	}
	byID := make(map[string]Addon, len(addons))
	for _, a := range addons {
		byID[a.ID] = a
	}

	tests := []struct {
		id          string
		wantKind    AddonKind
		arch        game.Arch
		wantURLTail string
		wantArchive bool
		wantOK      bool
	}{
		{
			// Per-arch keys holding real .addon files.
			id: "swap-chain-override-by-crosire", wantKind: AddonDirect,
			arch: game.ArchX64, wantURLTail: "swapchain_override.addon64",
			wantArchive: false, wantOK: true,
		},
		{
			id: "swap-chain-override-by-crosire", wantKind: AddonDirect,
			arch: game.ArchX86, wantURLTail: "swapchain_override.addon32",
			wantArchive: false, wantOK: true,
		},
		{
			// Per-arch keys holding ZIPs — the case a field-name-based
			// classifier gets wrong.
			id: "shadertoggler-by-otis-inf", wantKind: AddonZip,
			arch: game.ArchX86, wantURLTail: ".zip",
			wantArchive: true, wantOK: true,
		},
		{
			// 64-bit only: a 32-bit game has nothing to install.
			id: "igcs-connector-by-otis-inf", wantKind: AddonZip,
			arch: game.ArchX64, wantURLTail: ".zip",
			wantArchive: true, wantOK: true,
		},
		{
			id: "igcs-connector-by-otis-inf", wantKind: AddonZip,
			arch: game.ArchX86, wantOK: false,
		},
		{
			// A bare ".addon" extension is still a direct file.
			id: "frame-capture-by-murchalloo", wantKind: AddonDirect,
			arch: game.ArchX64, wantURLTail: ".addon",
			wantArchive: false, wantOK: true,
		},
		{
			// Arch-neutral zip serves both architectures.
			id: "reshadeeffectshadertoggler-rest-by-4lex4nder", wantKind: AddonZip,
			arch: game.ArchX86, wantURLTail: "release.zip",
			wantArchive: true, wantOK: true,
		},
		{
			id: "renodx-by-shortfuse", wantKind: AddonManual,
			arch: game.ArchX64, wantOK: false,
		},
	}

	for _, tt := range tests {
		a, ok := byID[tt.id]
		if !ok {
			t.Errorf("addon %q not found", tt.id)
			continue
		}
		if got := a.Kind(); got != tt.wantKind {
			t.Errorf("%s: Kind() = %q, want %q", tt.id, got, tt.wantKind)
		}
		if got := a.Installable(); got != (tt.wantKind != AddonManual) {
			t.Errorf("%s: Installable() = %v", tt.id, got)
		}

		src, ok := a.SourceFor(tt.arch)
		if ok != tt.wantOK {
			t.Errorf("%s/%s: SourceFor ok = %v, want %v", tt.id, tt.arch, ok, tt.wantOK)
			continue
		}
		if !tt.wantOK {
			continue
		}
		if !hasSuffix(src.URL, tt.wantURLTail) {
			t.Errorf("%s/%s: URL = %q, want suffix %q", tt.id, tt.arch, src.URL, tt.wantURLTail)
		}
		if src.Archive != tt.wantArchive {
			t.Errorf("%s/%s: Archive = %v, want %v", tt.id, tt.arch, src.Archive, tt.wantArchive)
		}
	}
}

// REST also ships shaders, via a key the plan never documented.
func TestAddonEffectInstallPath(t *testing.T) {
	addons, err := ParseAddons(fixture(t, "Addons.ini"))
	if err != nil {
		t.Fatalf("ParseAddons() error = %v", err)
	}
	for _, a := range addons {
		if a.ID != "reshadeeffectshadertoggler-rest-by-4lex4nder" {
			continue
		}
		if want := `.\reshade-shaders\Shaders\REST`; a.EffectInstallPath != want {
			t.Errorf("EffectInstallPath = %q, want %q", a.EffectInstallPath, want)
		}
		return
	}
	t.Fatal("REST addon not found in fixture")
}

func TestIsArchiveURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.invalid/release.zip", true},
		{"https://example.invalid/release.ZIP", true},
		{"https://example.invalid/release.zip?raw=true", true},
		{"https://example.invalid/x.addon64", false},
		{"https://example.invalid/x.addon32", false},
		{"https://example.invalid/x.addon", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isArchiveURL(tt.url); got != tt.want {
			t.Errorf("isArchiveURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
