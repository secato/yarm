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
	body := panelText(t, entry, Env{Styles: NewStyles(true), Width: 100, Height: 30})

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
		{Executable: game.Executable{Path: "Game/emberhollow.exe", Arch: game.ArchX64, API: game.APID3D12}},
		{Executable: game.Executable{Path: "Game/start_protected_game.exe", Arch: game.ArchX64}},
	}
	entry := GameEntry{
		Game:   game.Game{ID: "manual:x", Name: "Ember Hollow", Root: dir},
		Exes:   exes,
		Groups: groupByFolder(dir, exes),
	}

	env := Env{Styles: NewStyles(true), Width: 100, Height: 30}
	body := panelText(t, entry, env)

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

	body := reshadeStatusText(groups[0], Env{Styles: NewStyles(true), Width: 100, Height: 30}, "hint", 100)

	if !strings.Contains(body, "last seen running: 6.8.0") {
		t.Errorf("body should show the runtime-detected version:\n%s", body)
	}
	if !strings.Contains(body, "dxgi.dll (D3D10 / D3D11 / D3D12)") {
		t.Errorf("body should show which APIs the DLL covers:\n%s", body)
	}
	// Styled output interleaves ANSI codes between lines, so this checks
	// each line landed rather than one exact multi-line substring, and
	// that they were not comma-joined onto a single line instead.
	for _, want := range []string{"Shaders (2)", "standard-effects", "sweetfx-by-ceejay-dk"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "standard-effects, sweetfx-by-ceejay-dk") {
		t.Errorf("packages should be listed one per line, not comma-joined:\n%s", body)
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

	env := Env{Styles: NewStyles(true), Width: 100, Height: 30}
	gd := gamesWith(t, entry, env)

	foundUpdate := false
	for _, b := range gd.KeyBindings() {
		if strings.Contains(b.Help().Desc, "edit install") {
			foundUpdate = true
		}
	}
	if !foundUpdate {
		t.Error(`KeyBindings() should offer "edit install" since the folder already has an install`)
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
	if got := wiz.exe.Path; got != "Vantage.exe" {
		t.Errorf("wizard targets %q, want Vantage.exe (the one actually installed)", got)
	}
}

// A game with two distinct folders — one installed, one holding an
// unmanaged install — must let the cursor move between them, and offer
// the right action for whichever one is current.
func TestMultiFolderGameOffersEveryActionItsFoldersAllow(t *testing.T) {
	dir := t.TempDir()
	shipDir := filepath.Join(dir, "Ship")
	if err := os.MkdirAll(shipDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for name, body := range map[string]string{"dxgi.dll": "dll", "ReShade.ini": "[GENERAL]\n"} {
		if err := os.WriteFile(filepath.Join(shipDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	installed := state.Install{Exe: "Release/Game.exe", ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "normal", DLL: "dxgi.dll"}}
	exes := []Executable{
		{Executable: game.Executable{Path: "Release/Game.exe"}, Installed: &installed},
		{Executable: game.Executable{Path: "Ship/Game.exe"}},
	}
	entry := GameEntry{
		Game:   game.Game{ID: "manual:x", Name: "Two Folders", Root: dir},
		Exes:   exes,
		Groups: groupByFolder(dir, exes),
	}
	if len(entry.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(entry.Groups))
	}

	env := Env{Styles: NewStyles(true), Width: 100, Height: 30}
	gs := gamesWith(t, entry, env)

	// Both actions are offered from the list, because both are possible
	// somewhere in this game; which folder each applies to is the picker's
	// question, not a cursor's.
	offered := map[string]bool{}
	for _, b := range gs.KeyBindings() {
		offered[b.Help().Desc] = true
	}
	for _, want := range []string{"edit install", "adopt ReShade"} {
		if !offered[want] {
			t.Errorf("%q should be offered; bindings = %v", want, offered)
		}
	}

	// Only one folder has an install, so edit does not need to ask which:
	// it goes straight to the wizard on that folder.
	_, cmd := gs.Update(tea.KeyPressMsg{Code: 'e', Text: "e"}, env)
	if cmd == nil {
		t.Fatal("edit should act on the one installed folder")
	}
	push, ok := cmd().(pushScreenMsg)
	if !ok {
		t.Fatalf("message = %T, want pushScreenMsg", cmd())
	}
	if wiz, ok := push.screen.(*WizardScreen); !ok {
		t.Fatalf("screen = %T, want *WizardScreen", push.screen)
	} else if wiz.exe.Path != "Release/Game.exe" {
		t.Errorf("wizard targets %q, want the installed folder's exe", wiz.exe.Path)
	}

	// Installing could mean either folder, so that one does ask.
	_, cmd = gs.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, env)
	push, ok = cmd().(pushScreenMsg)
	if !ok {
		t.Fatalf("message = %T, want pushScreenMsg", cmd())
	}
	pick, ok := push.screen.(*FolderPickScreen)
	if !ok {
		t.Fatalf("screen = %T, want *FolderPickScreen", push.screen)
	}
	if len(pick.groups) != 2 {
		t.Errorf("picker offers %d folder(s), want both", len(pick.groups))
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
	if !groupIsAddon(addonInstalled) {
		t.Error("an installed add-on build should be flagged as addon")
	}

	normalInstalled := FolderGroup{Installed: &state.Install{
		ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "normal", DLL: "dxgi.dll"},
	}}
	if groupIsAddon(normalInstalled) {
		t.Error("a normal-flavor install should not be flagged as addon")
	}

	unmanagedAddon := FolderGroup{Unmanaged: &install.AdoptCandidate{DLLName: "dxgi.dll", HasAddons: true}}
	if !groupIsAddon(unmanagedAddon) {
		t.Error("an unmanaged install with add-on files should be flagged as addon")
	}
}

// The anti-cheat warning must appear once, at the very bottom of a
// folder's whole block — after its executables — not sandwiched between
// the ReShade status and the executables list.
func TestSidePanelShowsAntiCheatWarningBelowEverythingElse(t *testing.T) {
	installed := state.Install{
		Exe:     "game.exe",
		ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "addon", DLL: "dxgi.dll"},
	}
	exes := []Executable{{Executable: game.Executable{Path: "game.exe"}, Installed: &installed}}
	entry := GameEntry{
		Game:   game.Game{ID: "manual:x", Name: "X", Root: "/games/x"},
		Exes:   exes,
		Groups: groupByFolder("/games/x", exes),
	}

	env := Env{Styles: NewStyles(true), Width: 100, Height: 30}
	body := gamesWith(t, entry, env).View(env)

	execIdx := strings.Index(body, "Executables")
	warnIdx := strings.Index(body, "anti-cheat")
	if execIdx < 0 || warnIdx < 0 {
		t.Fatalf("expected both \"Executables\" and the anti-cheat warning in the body:\n%s", body)
	}
	if warnIdx < execIdx {
		t.Errorf("the anti-cheat warning should come after \"Executables\", not before:\n%s", body)
	}
}

// Pressing i on a folder with an unmanaged install must open the same
// adopt confirmation a presses, not attempt a fresh install over files
// that are already there.
func TestInstallKeyRedirectsToAdoptWhenUnmanaged(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"dxgi.dll": "dll", "ReShade.ini": "[GENERAL]\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	exes := []Executable{{Executable: game.Executable{Path: "game.exe"}}}
	entry := GameEntry{
		Game:   game.Game{ID: "manual:x", Name: "X", Root: dir},
		Exes:   exes,
		Groups: groupByFolder(dir, exes),
	}
	if entry.Groups[0].Unmanaged == nil {
		t.Fatal("setup: the folder should be detected as unmanaged")
	}

	env := Env{Styles: NewStyles(true), Width: 100, Height: 30}
	gs := gamesWith(t, entry, env)

	_, cmd := gs.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, env)
	if cmd == nil {
		t.Fatal("pressing i should still produce a command")
	}
	msg, ok := cmd().(showOverlayMsg)
	if !ok {
		t.Fatalf("message = %T, want showOverlayMsg (a confirm dialog)", cmd())
	}
	confirm, ok := msg.overlay.(confirmOverlay)
	if !ok || !strings.Contains(confirm.question, "Adopt") {
		t.Errorf("overlay = %+v, want an \"Adopt\" confirm dialog", msg.overlay)
	}
}

// The "what is in this folder" block is four groups of facts, not one long
// list: each section gets a blank line before it and a heading in the
// heading style, since a faint label above faint items reads as more items.
func TestReShadeStatusSeparatesItsSections(t *testing.T) {
	grp := FolderGroup{
		Dir: "Game",
		Installed: &state.Install{
			ReShade:  state.ReShadeInfo{Version: "6.8.0", Flavor: "normal", DLL: "dxgi.dll"},
			Packages: []string{"standard-effects", "sweetfx-by-ceejay-dk"},
			Addons:   []string{"shadertoggler-by-otis-inf"},
		},
		Runtime: install.RuntimeInfo{ActiveTechniques: []string{"LumaSharpen"}},
	}

	body := reshadeStatusText(grp, Env{Styles: NewStyles(true), Width: 60}, "", 60)
	for _, want := range []string{"ReShade", "Shaders (2)", "Add-ons (1)", "Enabled effects (1)"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing section heading %q:\n%s", want, body)
		}
	}
	// A blank line before each section but the first.
	if got := strings.Count(body, "\n\n"); got != 3 {
		t.Errorf("found %d section breaks, want 3 (one before each of the last three):\n%q", got, body)
	}
	if strings.HasPrefix(body, "\n") {
		t.Errorf("the block should not open with a blank line:\n%q", body)
	}
	// Headings must not be the same faint style as the items under them.
	plain := NewStyles(true)
	if strings.Contains(body, plain.Faint.Render("Shaders (2)")) {
		t.Error("a section heading styled like its own items is not a heading")
	}
}

// The add-on warning is a notice, not one more fact in the block above it:
// it gets a blank line on either side wherever it appears.
func TestAnticheatWarningIsItsOwnBlock(t *testing.T) {
	var b strings.Builder
	b.WriteString("something above\n")
	writeAnticheatWarning(&b, Env{Styles: NewStyles(true), Width: 80}, 79)

	got := b.String()
	if !strings.Contains(got, "above\n\n") {
		t.Errorf("the warning needs a blank line above it:\n%q", got)
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Errorf("the warning needs a blank line below it:\n%q", got)
	}
	if !strings.Contains(got, "anti-cheat detection") {
		t.Errorf("wrapping lost the warning's point:\n%s", got)
	}
}

// panelText renders the games list's side panel for one entry — the view
// that replaced the game detail screen, and now the only place a game's
// folders, status and executables are shown.
func panelText(t *testing.T, entry GameEntry, env Env) string {
	t.Helper()
	s := NewGamesScreen(fakeLoader{entries: []GameEntry{entry}}, fakeDeps(), false)
	s.resize(env)
	next, _ := s.Update(gamesLoadedMsg{entries: []GameEntry{entry}}, env)
	body, _ := next.(*GamesScreen).renderDetail(entry, env)
	return body
}

// gamesWith returns a games list with entry loaded and selected — the
// screen every action now runs from.
func gamesWith(t *testing.T, entry GameEntry, env Env) *GamesScreen {
	t.Helper()
	s := NewGamesScreen(fakeLoader{entries: []GameEntry{entry}}, fakeDeps(), false)
	s.resize(env)
	next, _ := s.Update(gamesLoadedMsg{entries: []GameEntry{entry}}, env)
	return next.(*GamesScreen)
}
