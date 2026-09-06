package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/state"
)

func TestScanUnmanagedFull(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")
	f.GameFile("Game/dxgi.dll", "dll body")
	f.GameFile("Game/"+ININame, "[GENERAL]\n")
	f.GameFile("Game/"+artifacts.D3DCompiler, "d3dcompiler body")
	f.GameFile("Game/swapchain.addon64", "addon body")
	f.GameFile("Game/reshade-shaders/Shaders/Deband.fx", "// deband")
	f.GameFile("Game/reshade-shaders/Shaders/ReShade.fxh", "// header")
	f.GameFile("Game/reshade-shaders/Textures/noise.png", "png")

	exe := game.Executable{Path: filepath.FromSlash("Game/emberhollow.exe"), Arch: game.ArchX64, API: game.APID3D12}
	c, ok := ScanUnmanaged(f.GameDir, exe)
	if !ok {
		t.Fatal("ScanUnmanaged() ok = false, want true")
	}

	if c.DLLName != "dxgi.dll" {
		t.Errorf("DLLName = %q, want dxgi.dll", c.DLLName)
	}
	if !c.HasD3DCompiler {
		t.Error("HasD3DCompiler should be true")
	}
	if !c.HasAddons {
		t.Error("HasAddons should be true")
	}
	if len(c.AddonFiles) != 1 || c.AddonFiles[0] != "Game/swapchain.addon64" {
		t.Errorf("AddonFiles = %v", c.AddonFiles)
	}
	if len(c.ShaderFiles) != 2 {
		t.Errorf("ShaderFiles = %v, want 2", c.ShaderFiles)
	}
	if len(c.TextureFiles) != 1 || c.TextureFiles[0] != "Game/reshade-shaders/Textures/noise.png" {
		t.Errorf("TextureFiles = %v", c.TextureFiles)
	}

	wantCount := 2 + 1 + 1 + 2 + 1 // dll+ini, d3dcompiler, addon, 2 shaders, 1 texture
	if got := c.FileCount(); got != wantCount {
		t.Errorf("FileCount() = %d, want %d", got, wantCount)
	}
}

// A bare install — just the DLL and the ini, nothing else — must still
// scan cleanly rather than erroring because reshade-shaders/ does not
// exist.
func TestScanUnmanagedBareInstall(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")
	f.GameFile("Game/dxgi.dll", "dll body")
	f.GameFile("Game/"+ININame, "[GENERAL]\n")

	exe := game.Executable{Path: filepath.FromSlash("Game/emberhollow.exe")}
	c, ok := ScanUnmanaged(f.GameDir, exe)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if len(c.ShaderFiles) != 0 || len(c.TextureFiles) != 0 || len(c.AddonFiles) != 0 {
		t.Errorf("expected nothing extra, got %+v", c)
	}
	if c.FileCount() != 2 {
		t.Errorf("FileCount() = %d, want 2 (dll + ini)", c.FileCount())
	}
}

func TestScanUnmanagedNotFound(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")
	exe := game.Executable{Path: filepath.FromSlash("Game/emberhollow.exe")}

	if _, ok := ScanUnmanaged(f.GameDir, exe); ok {
		t.Error("ok = true for a directory with nothing ReShade-shaped in it")
	}
}

// An exe at the game root (no subdirectory) must still be detected.
func TestScanUnmanagedAtGameRoot(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Ravenswatch.exe", "the game")
	f.GameFile("dxgi.dll", "dll body")
	f.GameFile(ININame, "[GENERAL]\n")

	exe := game.Executable{Path: "Ravenswatch.exe"}
	c, ok := ScanUnmanaged(f.GameDir, exe)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if c.DLLName != "dxgi.dll" {
		t.Errorf("DLLName = %q, want dxgi.dll", c.DLLName)
	}
}

func TestAdoptRecordsRealHashesAndTouchesNothing(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")
	f.GameFile("Game/dxgi.dll", "dll body")
	f.GameFile("Game/"+ININame, "[GENERAL]\n")
	f.GameFile("Game/reshade-shaders/Shaders/Deband.fx", "// deband")

	before := snapshot(t, f.GameDir)

	exe := game.Executable{Path: filepath.FromSlash("Game/emberhollow.exe"), Arch: game.ArchX64, API: game.APID3D12}
	candidate, ok := ScanUnmanaged(f.GameDir, exe)
	if !ok {
		t.Fatal("ScanUnmanaged ok = false")
	}

	g := game.Game{ID: "manual:abc123", Name: "Ember Hollow", Provider: "manual", Root: f.GameDir}
	in, err := Adopt(f.StateDir, g, exe, candidate)
	if err != nil {
		t.Fatalf("Adopt() error = %v", err)
	}

	// Adopt must never write, move or delete anything in the game
	// directory — only installs.json changes.
	if diff := diffSnapshots(before, snapshot(t, f.GameDir)); diff != "" {
		t.Errorf("the game directory changed during Adopt():\n%s", diff)
	}

	if len(in.Files) != 3 {
		t.Fatalf("Files = %v, want 3 (dll, ini, one shader)", in.Files)
	}
	for _, fl := range in.Files {
		if len(fl.SHA256) != 64 {
			t.Errorf("%s: sha256 = %q, want a real digest", fl.Path, fl.SHA256)
		}
	}
	if in.ReShade.Flavor != string(FlavorNormal) {
		t.Errorf("Flavor = %q, want normal (no addon files present)", in.ReShade.Flavor)
	}
	if in.ReShade.DLL != "dxgi.dll" {
		t.Errorf("DLL = %q", in.ReShade.DLL)
	}

	// The registry must now report this install as tracked.
	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("state.Load(): %v", err)
	}
	got, ok := reg.FindInstall(g.ID, "Game/emberhollow.exe")
	if !ok {
		t.Fatal("the adopted install should be findable in the registry")
	}
	if len(got.Files) != 3 {
		t.Errorf("recorded Files = %v, want 3", got.Files)
	}
}

// An addon file present anywhere in the exe directory means the addon
// build, and Adopt must record it accordingly and pick up the arch.
func TestAdoptDetectsAddonFlavor(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")
	f.GameFile("Game/dxgi.dll", "dll body")
	f.GameFile("Game/"+ININame, "[GENERAL]\n")
	f.GameFile("Game/swapchain.addon64", "addon body")

	exe := game.Executable{Path: filepath.FromSlash("Game/emberhollow.exe"), Arch: game.ArchX86}
	candidate, ok := ScanUnmanaged(f.GameDir, exe)
	if !ok {
		t.Fatal("ScanUnmanaged ok = false")
	}

	g := game.Game{ID: "manual:x", Name: "X", Provider: "manual", Root: f.GameDir}
	in, err := Adopt(f.StateDir, g, exe, candidate)
	if err != nil {
		t.Fatalf("Adopt(): %v", err)
	}
	if in.ReShade.Flavor != string(FlavorAddon) {
		t.Errorf("Flavor = %q, want addon", in.ReShade.Flavor)
	}
	if in.ReShade.Arch != string(game.ArchX86) {
		t.Errorf("Arch = %q, want %q", in.ReShade.Arch, game.ArchX86)
	}
}

// Adopting, then uninstalling through yarm, must remove exactly the
// adopted files and leave everything else byte-identical — proving the
// adopted record is fully equivalent to one yarm would have written
// itself.
func TestAdoptThenUninstallIsByteIdentical(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")
	f.GameFile("Game/data/assets.pak", "unrelated game data")
	f.GameFile("Game/dxgi.dll", "dll body")
	f.GameFile("Game/"+ININame, "[GENERAL]\n")
	f.GameFile("Game/reshade-shaders/Shaders/Deband.fx", "// deband")
	f.GameFile("Game/reshade-shaders/Textures/noise.png", "png")

	pristine := snapshot(t, f.GameDir)
	// Remove the ReShade-created entries to get what the game looked
	// like before a manual install, for the final comparison.
	delete(pristine, "Game/dxgi.dll")
	delete(pristine, "Game/"+ININame)
	delete(pristine, "Game/reshade-shaders/Shaders/Deband.fx")
	delete(pristine, "Game/reshade-shaders/Textures/noise.png")

	exe := game.Executable{Path: filepath.FromSlash("Game/emberhollow.exe"), Arch: game.ArchX64, API: game.APID3D12}
	candidate, ok := ScanUnmanaged(f.GameDir, exe)
	if !ok {
		t.Fatal("ScanUnmanaged ok = false")
	}
	g := game.Game{ID: "manual:x", Name: "X", Provider: "manual", Root: f.GameDir}
	if _, err := Adopt(f.StateDir, g, exe, candidate); err != nil {
		t.Fatalf("Adopt(): %v", err)
	}

	out, err := NewUninstaller(f.StateDir).Run(UninstallRequest{GameID: g.ID, Exe: "Game/emberhollow.exe"})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(out.Kept) != 0 {
		t.Errorf("Kept = %v, want nothing kept", out.Kept)
	}

	after := snapshot(t, f.GameDir)
	if diff := diffSnapshots(pristine, after); diff != "" {
		t.Errorf("not byte-identical after adopt+uninstall:\n%s", diff)
	}
	if _, statErr := os.Stat(filepath.Join(f.GameDir, "Game", "reshade-shaders")); statErr == nil {
		t.Error("the now-empty reshade-shaders tree should have been pruned")
	}
}

// diffSnapshots reports a human-readable difference between two
// path->hash snapshots, or "" if they match exactly.
func diffSnapshots(before, after map[string]string) string {
	var lines []string
	for path, hash := range before {
		if got, ok := after[path]; !ok {
			lines = append(lines, "removed: "+path)
		} else if got != hash {
			lines = append(lines, "changed: "+path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			lines = append(lines, "added: "+path)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}
