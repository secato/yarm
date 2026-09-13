package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/state"
)

// twoFolderGame has one folder with an install and one without.
func twoFolderGame() GameEntry {
	installed := state.Install{
		Exe:     "Release/Game.exe",
		ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "normal", DLL: "dxgi.dll"},
	}
	exes := []Executable{
		{Executable: game.Executable{Path: "Release/Game.exe"}, Installed: &installed},
		{Executable: game.Executable{Path: "Ship/Game.exe"}},
	}
	return GameEntry{
		Game:   game.Game{ID: "manual:x", Name: "Two Folders", Root: "/games/two"},
		Exes:   exes,
		Groups: groupByFolder("/games/two", exes),
	}
}

// A multi-select pathsList always keeps at least one folder checked:
// unchecking the last one would leave an edit or uninstall with nothing to
// apply to, which is not a smaller version of the action.
func TestPathsListKeepsAtLeastOneSelected(t *testing.T) {
	e := twoFolderGame()
	p := newPathsList(e.Groups, []string{"Release"}, true)

	p.toggle() // uncheck the only selected row
	if len(p.selectedGroups()) != 1 {
		t.Fatalf("toggling off the last checked folder should be a no-op, got %d selected", len(p.selectedGroups()))
	}

	p.down()
	p.toggle() // check Ship too
	if len(p.selectedGroups()) != 2 {
		t.Fatalf("selected = %d, want both folders checked", len(p.selectedGroups()))
	}

	p.up()
	p.toggle() // now Release can come off, since Ship is still checked
	if got := p.selectedGroups(); len(got) != 1 || got[0].Dir != "Ship" {
		t.Errorf("selected = %v, want just Ship", got)
	}
}

// Single-select mode is a radio: checking one folder clears every other.
func TestPathsListSingleSelectIsARadio(t *testing.T) {
	e := twoFolderGame()
	p := newPathsList(e.Groups, []string{"Release"}, false)

	p.down()
	p.toggle()
	got := p.selectedGroups()
	if len(got) != 1 || got[0].Dir != "Ship" {
		t.Errorf("selected = %v, want just Ship", got)
	}
}

// Executables show in full by default — few enough folders that showing
// every one's list does not cost anything — and toggling expand collapses
// the row under the cursor (only that one; the cursor starts on Release,
// so Ship's own list is untouched), then expands it back.
func TestPathsListToggleExpand(t *testing.T) {
	e := twoFolderGame()
	p := newPathsList(e.Groups, []string{"Release"}, true)
	env := Env{Styles: NewStyles(true), Width: 80, Height: 20}

	render := func() string {
		var b strings.Builder
		writePathsList(&b, env, p, env.Height)
		return b.String()
	}

	if n := strings.Count(render(), "Game.exe"); n != 2 {
		t.Errorf("a game with few folders should show every folder's executables by default, got %d, want 2:\n%s",
			n, render())
	}

	p.toggleExpand()
	if n := strings.Count(render(), "Game.exe"); n != 1 {
		t.Errorf("toggling expand should collapse just the folder under the cursor, got %d, want 1:\n%s",
			n, render())
	}

	p.toggleExpand()
	if n := strings.Count(render(), "Game.exe"); n != 2 {
		t.Errorf("toggling again should expand it back, got %d, want 2:\n%s", n, render())
	}
}

// dualArchGame is one folder — the game root — mixing a 32-bit launcher
// stub and the real 64-bit game, the exact shape Battle.net titles ship
// (Diablo IV: "Diablo IV Launcher.exe" beside "Diablo IV.exe") and the
// case primaryExe alone cannot safely resolve without a look from a human.
func dualArchGame() GameEntry {
	exes := []Executable{
		{Executable: game.Executable{Path: "Diablo IV Launcher.exe", Arch: game.ArchX86}},
		{Executable: game.Executable{Path: "Diablo IV.exe", Arch: game.ArchX64}},
	}
	return GameEntry{
		Game:   game.Game{ID: "battlenet:fenris", Name: "Diablo IV", Root: "/games/D4"},
		Exes:   exes,
		Groups: groupByFolder("/games/D4", exes),
	}
}

// A folder with two real candidates and nothing installed starts on
// primaryExe's guess (the x64 one), renders both marked so the choice is
// visible rather than silent, and cycleExe steps between them.
func TestPathsListCycleExeSwitchesTheChosenExecutable(t *testing.T) {
	e := dualArchGame()
	p := newPathsList(e.Groups, []string{""}, true)
	env := Env{Styles: NewStyles(true), Width: 80, Height: 20}

	render := func() string {
		var b strings.Builder
		writePathsList(&b, env, p, env.Height)
		return b.String()
	}

	if got := p.exeFor(e.Groups[0]).Path; got != "Diablo IV.exe" {
		t.Fatalf("exeFor() = %q before any cycling, want the x64 default", got)
	}
	if body := render(); !strings.Contains(body, "● Diablo IV.exe") || !strings.Contains(body, "○ Diablo IV Launcher.exe") {
		t.Errorf("the chosen exe should be marked and the other shown unmarked:\n%s", body)
	}

	p.cycleExe()
	if got := p.exeFor(e.Groups[0]).Path; got != "Diablo IV Launcher.exe" {
		t.Errorf("exeFor() = %q after cycling once, want the launcher", got)
	}
	if body := render(); !strings.Contains(body, "● Diablo IV Launcher.exe") {
		t.Errorf("cycling should move the marker to the launcher:\n%s", body)
	}

	p.cycleExe()
	if got := p.exeFor(e.Groups[0]).Path; got != "Diablo IV.exe" {
		t.Errorf("exeFor() = %q after cycling twice, want it wrapped back to the x64 exe", got)
	}
}

// cycleExe has nothing to do once a folder is no longer ambiguous — either
// it never was (one real candidate) or an install already fixed its exe.
func TestPathsListCycleExeIsNoOpWhenNotAmbiguous(t *testing.T) {
	installed := state.Install{Exe: "Diablo IV.exe"}
	exes := []Executable{
		{Executable: game.Executable{Path: "Diablo IV Launcher.exe", Arch: game.ArchX86}},
		{Executable: game.Executable{Path: "Diablo IV.exe", Arch: game.ArchX64}, Installed: &installed},
	}
	groups := groupByFolder("/games/D4", exes)
	p := newPathsList(groups, []string{""}, true)

	before := p.exeFor(groups[0]).Path
	p.cycleExe()
	if got := p.exeFor(groups[0]).Path; got != before {
		t.Errorf("cycleExe() changed the pick from %q to %q on an already-installed folder", before, got)
	}
	if got := p.exeFor(groups[0]).Path; got != "Diablo IV.exe" {
		t.Errorf("exeFor() = %q, want the installed exe regardless of primaryExe's guess", got)
	}
}

// Past a handful of folders, showing every one's executables in full
// would defeat the point of the pane — comparing folders at a glance —
// so it falls back to a count until a folder is expanded by hand.
func TestPathsListCollapsesByDefaultPastFiveFolders(t *testing.T) {
	exes := make([]Executable, 0, 6)
	for i := range 6 {
		exes = append(exes, Executable{Executable: game.Executable{
			Path: fmt.Sprintf("Folder%d/Game.exe", i),
		}})
	}
	groups := groupByFolder("/games/many", exes)
	p := newPathsList(groups, nil, true)
	env := Env{Styles: NewStyles(true), Width: 80, Height: 30}

	var b strings.Builder
	writePathsList(&b, env, p, env.Height)
	if strings.Contains(b.String(), "Game.exe") {
		t.Errorf("more than %d folders should default to collapsed:\n%s", maxAutoExpandedGroups, b.String())
	}
}

// Checking a folder that was not checked when the wizard opened is an
// addition; unchecking one that already has a recorded install and was
// checked at the start is a removal. Round-tripping a folder — checking or
// unchecking it and then undoing that — must net out to neither.
func TestPathsListTracksAddedAndRemovedFolders(t *testing.T) {
	e := twoFolderGame()
	p := newPathsList(e.Groups, []string{"Release"}, true)

	if len(p.addedFolders()) != 0 || len(p.removedInstalls()) != 0 {
		t.Fatalf("nothing has changed yet: added=%v removed=%v", p.addedFolders(), p.removedInstalls())
	}

	// Check Ship (new), uncheck Release (already installed, was checked).
	p.down()
	p.toggle() // Ship on
	p.up()
	p.toggle() // Release off

	if got := p.addedFolders(); len(got) != 1 || got[0].Dir != "Ship" {
		t.Errorf("addedFolders() = %v, want just Ship", got)
	}
	if got := p.removedInstalls(); len(got) != 1 || got[0].Dir != "Release" {
		t.Errorf("removedInstalls() = %v, want just Release", got)
	}

	// Check Release back on: round-tripped, so neither list should still
	// name it.
	p.toggle()
	if got := p.removedInstalls(); len(got) != 0 {
		t.Errorf("removedInstalls() = %v, want none — Release was checked again", got)
	}
}

// The whole point of the Paths step: checking a second folder on it makes
// Apply run the edit against both, one folderOp each, rather than only the
// folder the wizard happened to open on.
func TestWizardAppliesToEveryFolderCheckedOnThePathsStep(t *testing.T) {
	e := twoFolderGame()
	s := loadWizard(t, e, 0, fakeDeps())
	if s.step != stepHub {
		t.Fatalf("a folder with a recorded install opens editing, step = %v", s.step)
	}
	if len(s.paths.selectedGroups()) != 1 {
		t.Fatalf("a freshly opened edit should start with just its reference folder checked")
	}

	// Check the second folder too from the Paths section.
	s = openSection(t, s, stepPaths)
	s = pressSpecial(t, s, tea.KeyDown)
	s = pressSpecial(t, s, tea.KeySpace)
	if len(s.paths.selectedGroups()) != 2 {
		t.Fatalf("checking the second folder should select both")
	}

	ops, ok := s.buildOps()
	if !ok {
		t.Fatal("buildOps() should succeed with both folders checked")
	}
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want one per checked folder", len(ops))
	}
	dirs := map[string]bool{}
	for _, op := range ops {
		dirs[op.Dir] = true
		if op.Install == nil {
			t.Errorf("op for %q should be an install", op.Dir)
		}
	}
	if !dirs["Release"] || !dirs["Ship"] {
		t.Errorf("ops cover %v, want Release and Ship", dirs)
	}
}

// Tabbing on the Paths step to switch a folder's chosen executable must
// carry all the way through to what actually gets installed: the request
// buildOps hands to the executor, and (for the reference folder) the
// architecture the DLL step's recommendation is based on. This is the
// fix for installing the wrong-bitness ReShade build into a folder like
// Diablo IV's, which mixes a 32-bit launcher stub with the real 64-bit
// game and used to leave primaryExe to guess between them silently.
func TestTabOnPathsStepSwitchesTheInstalledExecutable(t *testing.T) {
	e := dualArchGame()
	s := loadWizardAtPaths(t, e, 1, fakeDeps()) // preselect Diablo IV.exe (x64)
	if s.exe.Path != "Diablo IV.exe" {
		t.Fatalf("wizard opened on %q, want Diablo IV.exe", s.exe.Path)
	}

	s = pressSpecial(t, s, tea.KeyTab)
	if s.exe.Path != "Diablo IV Launcher.exe" {
		t.Fatalf("tab should switch exe to the launcher, s.exe = %q", s.exe.Path)
	}
	if got := s.paths.exeFor(s.paths.groups[0]).Path; got != "Diablo IV Launcher.exe" {
		t.Errorf("paths.exeFor() = %q, want it to agree with s.exe", got)
	}

	s = advance(t, s, stepReview)
	ops, ok := s.buildOps()
	if !ok {
		t.Fatal("buildOps() should succeed")
	}
	if len(ops) != 1 || ops[0].Install == nil {
		t.Fatalf("ops = %+v, want exactly one install", ops)
	}
	if got := ops[0].Install.Exe.Path; got != "Diablo IV Launcher.exe" {
		t.Errorf("request targets %q, want the tabbed-to launcher", got)
	}
	if got := ops[0].Install.Exe.Arch; got != game.ArchX86 {
		t.Errorf("request arch = %q, want x86 (the launcher's own)", got)
	}
}
