package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	if os.Getuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}

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

// The games list and the detail views must both show this distinctly
// from an ordinary empty or native-build game.
func TestScanErrorRendersDistinctlyFromNativeBuild(t *testing.T) {
	entry := GameEntry{ScanErr: os.ErrPermission}
	gd := NewGameDetailScreen(entry, Deps{})
	body := gd.View(Env{Styles: NewStyles(true), Width: 100, Height: 30})

	if !strings.Contains(body, "Could not scan") || !strings.Contains(body, "Permission denied") {
		t.Errorf("detail view should explain the scan error:\n%s", body)
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
		{Executable: game.Executable{Path: "Game/eldenring.exe"}},
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

// The detail screen must say what's found once per folder, not once per
// executable — the bug this whole grouping model exists to fix.
func TestGameDetailShowsOneUnmanagedStatusNotPerExecutable(t *testing.T) {
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
		{Executable: game.Executable{Path: "Game/eldenring.exe", Arch: game.ArchX64, API: game.APID3D12}},
		{Executable: game.Executable{Path: "Game/start_protected_game.exe", Arch: game.ArchX64}},
	}
	entry := GameEntry{
		Game:   game.Game{ID: "manual:x", Name: "ELDEN RING", Root: dir},
		Exes:   exes,
		Groups: groupByFolder(dir, exes),
	}

	gd := NewGameDetailScreen(entry, Deps{})
	env := Env{Styles: NewStyles(true), Width: 100, Height: 30}
	gd.resize(env)
	body := gd.View(env)

	if n := strings.Count(body, "found, untracked"); n != 1 {
		t.Errorf("body mentions \"found, untracked\" %d time(s), want exactly 1:\n%s", n, body)
	}
	if !strings.Contains(body, "start_protected_game.exe") {
		t.Error("both executables should still be listed, just without a repeated status")
	}
}

// An adopted install's real version cannot be recorded at adopt time, but
// ReShade's own log (read once the game has run) can supply it — the
// ReShade section should show that as a "last seen" line, alongside which
// APIs the DLL covers and packages listed one per line rather than
// comma-joined.
func TestWriteReShadeStatusShowsRuntimeInfoAndListsPackages(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "Game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	logLine := "Initializing crosire's ReShade version '6.8.0' (64-bit) loaded from 'dxgi.dll' into 'x.exe' ...\n"
	if err := os.WriteFile(filepath.Join(gameDir, "ReShade.log"), []byte(logLine), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	installed := state.Install{
		Exe:      "Game/x.exe",
		ReShade:  state.ReShadeInfo{Version: install.AdoptedVersion, Flavor: "normal", DLL: "dxgi.dll"},
		Packages: []string{"standard-effects", "sweetfx-by-ceejay-dk"},
	}
	exes := []Executable{{Executable: game.Executable{Path: "Game/x.exe"}, Installed: &installed}}
	groups := groupByFolder(dir, exes)
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}

	var b strings.Builder
	writeReShadeStatus(&b, groups[0], Env{Styles: NewStyles(true), Width: 100, Height: 30}, "hint")
	body := b.String()

	if !strings.Contains(body, "last seen running: 6.8.0") {
		t.Errorf("body should show the runtime-detected version:\n%s", body)
	}
	if !strings.Contains(body, "dxgi.dll (D3D10 / D3D11 / D3D12)") {
		t.Errorf("body should show which APIs the DLL covers:\n%s", body)
	}
	// Styled output interleaves ANSI codes between lines, so this checks
	// each line landed rather than one exact multi-line substring, and
	// that they were not comma-joined onto a single line instead.
	for _, want := range []string{"packages:", "standard-effects", "sweetfx-by-ceejay-dk"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "standard-effects, sweetfx-by-ceejay-dk") {
		t.Errorf("packages should be listed one per line, not comma-joined:\n%s", body)
	}
}

// Three executables sharing one folder (Control.exe, Control_DX11.exe,
// Control_DX12.exe) must all offer "update ReShade" once any one of them
// is installed, and pressing i must always edit the actual install
// regardless of which sibling is highlighted — ReShade applies to the
// whole folder, and creating a second install.json entry for a sibling
// executable over the same files would leave both entries claiming files
// only one of them can safely own.
func TestFolderLevelInstallTargetsTheActuallyInstalledExe(t *testing.T) {
	installed := state.Install{
		Exe:     "Control.exe",
		ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "addon", DLL: "dxgi.dll"},
	}
	exes := []Executable{
		{Executable: game.Executable{Path: "Control.exe", Arch: game.ArchX64}, Installed: &installed},
		{Executable: game.Executable{Path: "Control_DX11.exe", Arch: game.ArchX64, API: game.APID3D11}},
		{Executable: game.Executable{Path: "Control_DX12.exe", Arch: game.ArchX64, API: game.APID3D12}},
	}
	entry := GameEntry{
		Game:   game.Game{ID: "steam:870780", Name: "Control", Provider: "steam", Root: "/games/Control"},
		Exes:   exes,
		Groups: groupByFolder("/games/Control", exes),
	}

	gd := NewGameDetailScreen(entry, fakeDeps())
	env := Env{Styles: NewStyles(true), Width: 100, Height: 30}
	gd.resize(env)
	gd.table.SetCursor(2) // Control_DX12.exe — not the one actually installed

	foundUpdate := false
	for _, b := range gd.KeyBindings() {
		if strings.Contains(b.Help().Desc, "update ReShade") {
			foundUpdate = true
		}
	}
	if !foundUpdate {
		t.Error(`KeyBindings() should offer "update ReShade" even with a sibling exe highlighted`)
	}

	_, cmd := gd.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, env)
	if cmd == nil {
		t.Fatal("pressing i should push the wizard")
	}
	push, ok := cmd().(pushScreenMsg)
	if !ok {
		t.Fatalf("message = %T, want pushScreenMsg", cmd())
	}
	wiz, ok := push.screen.(*WizardScreen)
	if !ok {
		t.Fatalf("pushed screen = %T, want *WizardScreen", push.screen)
	}
	if got, ok := wiz.selectedExe(); !ok || got.Path != "Control.exe" {
		t.Errorf("wizard preselected %+v, want Control.exe (the one actually installed)", got)
	}
}
