package app

import (
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

// One eligible folder is not a question, so it is not asked: the action
// runs directly. This is what keeps the common case — one folder, one
// install — a single keypress.
func TestPickFolderSkipsTheQuestionForOneFolder(t *testing.T) {
	e := twoFolderGame()

	var got FolderGroup
	cmd := pickFolder(e, fakeDeps(), verbEdit, groupsWithInstall(e),
		func(_ GameEntry, grp FolderGroup, _ Deps) tea.Cmd {
			got = grp
			return nil
		})
	if cmd != nil {
		t.Errorf("expected the action to run directly, got a command: %T", cmd())
	}
	if got.Dir != "Release" {
		t.Errorf("acted on %q, want the only installed folder", got.Dir)
	}
}

// No eligible folder means the key should not have been offered; nothing
// happens rather than an empty picker.
func TestPickFolderDoesNothingWithNoEligibleFolder(t *testing.T) {
	if cmd := pickFolder(twoFolderGame(), fakeDeps(), verbAdopt, nil, nil); cmd != nil {
		t.Error("an action with no eligible folder should produce no command")
	}
}

// With several, the picker asks — and says enough about each folder to
// choose between them without opening either.
func TestPickerListsWhatIsInEachFolder(t *testing.T) {
	e := twoFolderGame()
	cmd := pickFolder(e, fakeDeps(), verbInstall, installableGroups(e), startInstallForGroup)
	push, ok := cmd().(pushScreenMsg)
	if !ok {
		t.Fatalf("message = %T, want pushScreenMsg", cmd())
	}
	pick := push.screen.(*FolderPickScreen)

	body := pick.View(Env{Styles: NewStyles(true), Width: 80, Height: 20})
	for _, want := range []string{"Install ReShade into which folder?", "Release/", "Ship/", "ReShade 6.8.0 (normal)", "no ReShade"} {
		if !strings.Contains(body, want) {
			t.Errorf("the picker should show %q:\n%s", want, body)
		}
	}

	// Enter runs the action on the highlighted folder.
	next, _ := pick.Update(tea.KeyPressMsg{Code: tea.KeyDown}, wizardEnv())
	_, cmd = next.(*FolderPickScreen).Update(tea.KeyPressMsg{Code: tea.KeyEnter}, wizardEnv())
	if cmd == nil {
		t.Fatal("enter should act on the highlighted folder")
	}
	push, ok = cmd().(pushScreenMsg)
	if !ok {
		t.Fatalf("message = %T, want pushScreenMsg", cmd())
	}
	wiz, ok := push.screen.(*WizardScreen)
	if !ok {
		t.Fatalf("screen = %T, want *WizardScreen", push.screen)
	}
	if wiz.exe.Path != "Ship/Game.exe" {
		t.Errorf("wizard targets %q, want the second folder", wiz.exe.Path)
	}
}
