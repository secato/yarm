package app

import (
	"strings"
	"testing"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/state"
)

// uninstallTestEntry returns an entry with one installed executable and
// the matching registry written to a temp state dir.
func uninstallTestEntry(t *testing.T) (GameEntry, string) {
	t.Helper()
	entry := GameEntry{
		Game: game.Game{ID: "steam:1", Name: "G", Provider: "steam", Root: testRoot("/games/g")},
		Exes: []Executable{{
			Executable: game.Executable{Path: "Game/g.exe", Arch: game.ArchX64, API: game.APID3D12},
		}},
	}
	in := state.Install{
		Exe:     "Game/g.exe",
		ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "normal", DLL: "dxgi.dll"},
		Files: []state.File{
			{Path: "Game/dxgi.dll", SHA256: strings.Repeat("ab", 32), Size: 100, Origin: state.OriginReShade},
			{Path: "Game/ReShade.ini", SHA256: strings.Repeat("cd", 32), Size: 200, Origin: state.OriginINI},
		},
	}
	entry.Exes[0].Installed = &in
	entry.Groups = groupByFolder(entry.Root, entry.Exes)

	stateDir := t.TempDir()
	reg := state.Registry{}
	gm := entry.Game
	reg.Record(entry.ID, state.Game{Name: gm.Name, Provider: gm.Provider, Root: gm.Root}, in)
	if err := state.Save(stateDir, reg); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	return entry, stateDir
}

// The uninstall confirmation names what it will remove: the recorded
// files, so "removes only the files yarm created" is checkable rather
// than taken on trust.
func TestUninstallConfirmListsRecordedFiles(t *testing.T) {
	entry, stateDir := uninstallTestEntry(t)
	deps := fakeDeps()
	deps.StateDir = stateDir

	cmd := startUninstall(entry, groupsWithInstall(entry), deps)
	if cmd == nil {
		t.Fatal("want an uninstall confirmation command, got nil")
	}
	msg, ok := cmd().(showOverlayMsg)
	if !ok {
		t.Fatalf("message = %T, want showOverlayMsg", cmd())
	}
	confirm, ok := msg.overlay.(confirmOverlay)
	if !ok {
		t.Fatalf("overlay = %T, want confirmOverlay", msg.overlay)
	}
	for _, want := range []string{"2 recorded file(s)", "Game/dxgi.dll", "Game/ReShade.ini"} {
		if !strings.Contains(confirm.detail, want) {
			t.Errorf("confirmation should name %q:\n%s", want, confirm.detail)
		}
	}
}

// Without a readable registry the confirmation falls back to the generic
// promise instead of blocking the uninstall.
func TestUninstallConfirmWithoutRegistryStaysGeneric(t *testing.T) {
	entry, _ := uninstallTestEntry(t)
	deps := fakeDeps()
	deps.StateDir = t.TempDir() // empty: no registry yet

	cmd := startUninstall(entry, groupsWithInstall(entry), deps)
	if cmd == nil {
		t.Fatal("want an uninstall confirmation command, got nil")
	}
	msg := cmd().(showOverlayMsg)
	confirm := msg.overlay.(confirmOverlay)
	if !strings.Contains(confirm.detail, "only the files yarm created") {
		t.Errorf("confirmation should fall back to the generic promise:\n%s", confirm.detail)
	}
	if strings.Contains(confirm.detail, "recorded file(s)") {
		t.Errorf("confirmation should not claim a file list it cannot read:\n%s", confirm.detail)
	}
}
