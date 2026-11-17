package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/platform"
	"github.com/secato/yarm/internal/platform/manual"
	"github.com/secato/yarm/internal/state"
)

// A game folder that cannot even be scanned (most commonly a permissions
// problem) must surface why, not just look like an empty game — the
// plan's "friendly errors: permission denied on game dir" case.
func TestLoadGamesSurfacesScanError(t *testing.T) {
	requirePOSIXPermissions(t)

	dir := t.TempDir()
	locked := filepath.Join(dir, "LockedGame")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	loader := ProviderLoader{
		Providers: []platform.Provider{manual.New([]manual.Entry{{Name: "Locked Game", Path: locked}})},
		StateDir:  t.TempDir(),
	}

	entries, err := loader.LoadGames(context.Background())
	if err != nil {
		t.Fatalf("LoadGames() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	e := entries[0]
	if e.ScanErr == nil {
		t.Fatal("ScanErr should be set for a folder that cannot be read")
	}
	if len(e.Exes) != 0 {
		t.Errorf("Exes = %v, want none", e.Exes)
	}
	if e.NativeBuild {
		t.Error("a scan error should not also trigger a native-build probe")
	}
}

// ReShade intercepts by directory, not by executable: two executables
// sharing one folder must produce a single group with one ReShade status,
// not one finding per executable.
func TestGroupByFolderSharesOneReShadeStatusPerDirectory(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "Game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for name, body := range map[string]string{"dxgi.dll": "dll body", "ReShade.ini": "[GENERAL]\n"} {
		if err := os.WriteFile(filepath.Join(gameDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	exes := []Executable{
		{Executable: game.Executable{Path: "Game/emberhollow.exe"}},
		{Executable: game.Executable{Path: "Game/start_protected_game.exe"}},
	}
	groups := groupByFolder(dir, exes)

	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 (both exes share Game/)", len(groups))
	}
	if groups[0].Unmanaged == nil {
		t.Fatal("the shared folder should be flagged unmanaged")
	}
	if len(groups[0].Exes) != 2 {
		t.Errorf("group has %d exes, want 2", len(groups[0].Exes))
	}
}

// Three executables sharing one folder (Vantage.exe, Vantage_DX11.exe,
// Vantage_DX12.exe) have exactly one install between them: pressing i
// must edit the actual install regardless of which of the three the
// registry happens to name — there is no per-executable selection to get
// wrong, since ReShade applies to the whole folder and there is only one
// folder here.
func TestFolderLevelInstallTargetsTheActuallyInstalledExe(t *testing.T) {
	installed := state.Install{
		Exe:     "Vantage.exe",
		ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "addon", DLL: "dxgi.dll"},
	}
	exes := []Executable{
		{Executable: game.Executable{Path: "Vantage.exe", Arch: game.ArchX64}, Installed: &installed},
		{Executable: game.Executable{Path: "Vantage_DX11.exe", Arch: game.ArchX64, API: game.APID3D11}},
		{Executable: game.Executable{Path: "Vantage_DX12.exe", Arch: game.ArchX64, API: game.APID3D12}},
	}
	entry := GameEntry{
		Game:   game.Game{ID: "steam:700220", Name: "Vantage Point", Provider: "steam", Root: "/games/Vantage"},
		Exes:   exes,
		Groups: groupByFolder("/games/Vantage", exes),
	}
	if len(entry.Groups) != 1 {
		t.Fatalf("groups = %d, want 1 (all three exes share the game root)", len(entry.Groups))
	}

	g := entry.Groups[0]
	exe, ok := g.InstalledExe()
	if !ok || exe.Path != "Vantage.exe" {
		t.Errorf("InstalledExe() = %q, %v; want Vantage.exe, the one actually installed", exe.Path, ok)
	}
}

// Battle.net bundles a native, 32-bit Launcher.exe beside the real 64-bit
// game in the same folder (Diablo IV is the case that surfaced this):
// alphabetically "Launcher.exe" sorts first and neither name matches
// skipExeName, so PrimaryExe used to hand back the 32-bit stub — which
// then had the wrong-bitness ReShade build installed for it, silently,
// since nothing checks a proxy DLL's bitness against the process that
// will actually load it.
func TestPrimaryExePrefersX64OverAlphabeticalOrder(t *testing.T) {
	exes := []Executable{
		{Executable: game.Executable{Path: "Diablo IV Launcher.exe", Arch: game.ArchX86}},
		{Executable: game.Executable{Path: "Diablo IV.exe", Arch: game.ArchX64}},
	}
	groups := groupByFolder("/games/D4", exes)
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	if got := groups[0].PrimaryExe().Path; got != "Diablo IV.exe" {
		t.Errorf("PrimaryExe() = %q, want the x64 exe over the alphabetically-first x86 one", got)
	}
}

// Skipped executables (uninstallers, redistributables) never count toward
// PrimaryExe's x64 preference or NeedsExeChoice's ambiguity — they are
// never something ReShade could attach to.
func TestPrimaryExeStillSkipsNonGameExecutables(t *testing.T) {
	exes := []Executable{
		{Executable: game.Executable{Path: "unins000.exe", Arch: game.ArchX64, Skipped: true}},
		{Executable: game.Executable{Path: "Game.exe", Arch: game.ArchX86}},
	}
	groups := groupByFolder("/games/G", exes)
	if got := groups[0].PrimaryExe().Path; got != "Game.exe" {
		t.Errorf("PrimaryExe() = %q, want the one non-skipped exe despite it being x86", got)
	}
}

// NeedsExeChoice is what tells the Paths step to surface (and let tab
// cycle) a folder's executable choice instead of silently trusting
// PrimaryExe: only when there is more than one real candidate and nothing
// is installed yet. An install already fixes which exe applies, and a
// folder with one candidate has nothing to choose between.
func TestNeedsExeChoice(t *testing.T) {
	installed := state.Install{Exe: "Diablo IV.exe"}
	cases := []struct {
		name string
		exes []Executable
		want bool
	}{
		{
			name: "two real candidates, nothing installed",
			exes: []Executable{
				{Executable: game.Executable{Path: "Diablo IV Launcher.exe", Arch: game.ArchX86}},
				{Executable: game.Executable{Path: "Diablo IV.exe", Arch: game.ArchX64}},
			},
			want: true,
		},
		{
			name: "two candidates but one already installed",
			exes: []Executable{
				{Executable: game.Executable{Path: "Diablo IV Launcher.exe", Arch: game.ArchX86}},
				{Executable: game.Executable{Path: "Diablo IV.exe", Arch: game.ArchX64}, Installed: &installed},
			},
			want: false,
		},
		{
			name: "one real candidate, one skipped",
			exes: []Executable{
				{Executable: game.Executable{Path: "unins000.exe", Skipped: true}},
				{Executable: game.Executable{Path: "Game.exe"}},
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			groups := groupByFolder("/games/G", tc.exes)
			if got := groups[0].NeedsExeChoice(); got != tc.want {
				t.Errorf("NeedsExeChoice() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The add-on build can trip anti-cheat detection, which is a real
// account-ban risk in an online game — the warning must show for both an
// already-tracked add-on install and an unmanaged one found with add-on
// files, but not for a plain normal-flavor install.
func TestGroupIsAddonDetectsAddonAntiCheatRisk(t *testing.T) {
	addonInstalled := FolderGroup{Installed: &state.Install{
		ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "addon", DLL: "dxgi.dll"},
	}}
	if !GroupIsAddon(addonInstalled) {
		t.Error("an installed add-on build should be flagged as addon")
	}

	normalInstalled := FolderGroup{Installed: &state.Install{
		ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "normal", DLL: "dxgi.dll"},
	}}
	if GroupIsAddon(normalInstalled) {
		t.Error("a normal-flavor install should not be flagged as addon")
	}

	unmanagedAddon := FolderGroup{Unmanaged: &install.AdoptCandidate{DLLName: "dxgi.dll", HasAddons: true}}
	if !GroupIsAddon(unmanagedAddon) {
		t.Error("an unmanaged install with add-on files should be flagged as addon")
	}
}
