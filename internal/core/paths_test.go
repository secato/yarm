package core

import (
	"testing"

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

// A multi-select picker always keeps at least one folder checked:
// unchecking the last one would leave an edit or uninstall with nothing to
// apply to, which is not a smaller version of the action.
func TestFolderPickerKeepsAtLeastOneSelected(t *testing.T) {
	e := twoFolderGame()
	p := NewFolderPicker(e.Groups, []string{"Release"}, true)

	p.Toggle("Release") // uncheck the only selected row
	if len(p.SelectedGroups()) != 1 {
		t.Fatalf("toggling off the last checked folder should be a no-op, got %d selected", len(p.SelectedGroups()))
	}

	p.Toggle("Ship") // check Ship too
	if len(p.SelectedGroups()) != 2 {
		t.Fatalf("selected = %d, want both folders checked", len(p.SelectedGroups()))
	}

	p.Toggle("Release") // now Release can come off, since Ship is still checked
	if got := p.SelectedGroups(); len(got) != 1 || got[0].Dir != "Ship" {
		t.Errorf("selected = %v, want just Ship", got)
	}
}

// Single-select mode is a radio: checking one folder clears every other.
func TestFolderPickerSingleSelectIsARadio(t *testing.T) {
	e := twoFolderGame()
	p := NewFolderPicker(e.Groups, []string{"Release"}, false)

	p.Toggle("Ship")
	got := p.SelectedGroups()
	if len(got) != 1 || got[0].Dir != "Ship" {
		t.Errorf("selected = %v, want just Ship", got)
	}
}

// A folder the picker does not know is ignored rather than checked.
func TestFolderPickerIgnoresUnknownFolders(t *testing.T) {
	e := twoFolderGame()
	p := NewFolderPicker(e.Groups, []string{"Release"}, true)

	p.Toggle("Nowhere")
	if p.Checked("Nowhere") {
		t.Error("an unknown folder was checked")
	}
}

// dualArchGame is one folder — the game root — mixing a 32-bit launcher
// stub and the real 64-bit game, the exact shape Battle.net titles ship
// (Diablo IV: "Diablo IV Launcher.exe" beside "Diablo IV.exe") and the
// case PrimaryExe alone cannot safely resolve without a look from a human.
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
// PrimaryExe's guess (the x64 one), and CycleExe steps between them.
func TestFolderPickerCycleExeSwitchesTheChosenExecutable(t *testing.T) {
	e := dualArchGame()
	p := NewFolderPicker(e.Groups, []string{""}, true)

	if got := p.ExeFor(e.Groups[0]).Path; got != "Diablo IV.exe" {
		t.Fatalf("ExeFor() = %q before any cycling, want the x64 default", got)
	}

	p.CycleExe("")
	if got := p.ExeFor(e.Groups[0]).Path; got != "Diablo IV Launcher.exe" {
		t.Errorf("ExeFor() = %q after cycling once, want the launcher", got)
	}

	p.CycleExe("")
	if got := p.ExeFor(e.Groups[0]).Path; got != "Diablo IV.exe" {
		t.Errorf("ExeFor() = %q after cycling twice, want it wrapped back to the x64 exe", got)
	}
}

// CycleExe has nothing to do once a folder is no longer ambiguous — either
// it never was (one real candidate) or an install already fixed its exe.
func TestFolderPickerCycleExeIsNoOpWhenNotAmbiguous(t *testing.T) {
	installed := state.Install{Exe: "Diablo IV.exe"}
	exes := []Executable{
		{Executable: game.Executable{Path: "Diablo IV Launcher.exe", Arch: game.ArchX86}},
		{Executable: game.Executable{Path: "Diablo IV.exe", Arch: game.ArchX64}, Installed: &installed},
	}
	groups := groupByFolder("/games/D4", exes)
	p := NewFolderPicker(groups, []string{""}, true)

	before := p.ExeFor(groups[0]).Path
	p.CycleExe("")
	if got := p.ExeFor(groups[0]).Path; got != before {
		t.Errorf("CycleExe() changed the pick from %q to %q on an already-installed folder", before, got)
	}
	if got := p.ExeFor(groups[0]).Path; got != "Diablo IV.exe" {
		t.Errorf("ExeFor() = %q, want the installed exe regardless of PrimaryExe's guess", got)
	}
}

// Checking a folder that was not checked when the picker opened is an
// addition; unchecking one that already has a recorded install and was
// checked at the start is a removal. Round-tripping a folder — checking or
// unchecking it and then undoing that — must net out to neither.
func TestFolderPickerTracksAddedAndRemovedFolders(t *testing.T) {
	e := twoFolderGame()
	p := NewFolderPicker(e.Groups, []string{"Release"}, true)

	if len(p.Added()) != 0 || len(p.Removed()) != 0 {
		t.Fatalf("nothing has changed yet: added=%v removed=%v", p.Added(), p.Removed())
	}

	// Check Ship (new), uncheck Release (already installed, was checked).
	p.Toggle("Ship")
	p.Toggle("Release")

	if got := p.Added(); len(got) != 1 || got[0].Dir != "Ship" {
		t.Errorf("Added() = %v, want just Ship", got)
	}
	if got := p.Removed(); len(got) != 1 || got[0].Dir != "Release" {
		t.Errorf("Removed() = %v, want just Release", got)
	}

	// Check Release back on: round-tripped, so neither list should still
	// name it.
	p.Toggle("Release")
	if got := p.Removed(); len(got) != 0 {
		t.Errorf("Removed() = %v, want none — Release was checked again", got)
	}
}
