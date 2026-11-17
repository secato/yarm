package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/secato/yarm/internal/config"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

func TestScanCustomContentSplitsKinds(t *testing.T) {
	root := t.TempDir()
	shaders := filepath.Join(root, "shaders", "FilmGrain", "Shaders")
	addons := filepath.Join(root, "addons", "overlay")
	for _, d := range []string{shaders, addons} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(shaders, "grain.fx"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(addons, "my.addon64"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := ScanCustomContent(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Shaders) != 1 || len(f.Addons) != 1 || f.Dir != root {
		t.Errorf("found = %+v", f)
	}
	// A folder that does not exist is not an error: most people never make one.
	if f, err := ScanCustomContent(filepath.Join(root, "nope")); err != nil || len(f.Shaders)+len(f.Addons) != 0 {
		t.Errorf("missing folder: %+v, %v", f, err)
	}
}

func TestCheckGameFolder(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, in string
		problem  string
		ok       bool
		message  string
	}{
		{"empty is nothing yet", "", "", false, ""},
		{"relative", "games/x", "Enter an absolute path.", false, "Enter an absolute path."},
		{"missing", filepath.Join(dir, "nope"), "No such folder.", false, "No such folder."},
		{"a file", file, "That is a file, not a folder.", false, "That is a file, not a folder."},
		{"no executables", dir, "", false, "No game executable found here. Pick the folder that holds the .exe."},
	}
	for _, tt := range tests {
		got := CheckGameFolder(tt.in)
		if got.Problem != tt.problem || got.OK() != tt.ok || got.Message() != tt.message {
			t.Errorf("%s: %+v, ok %v, message %q", tt.name, got, got.OK(), got.Message())
		}
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := ExpandPath("~/Games"); got != filepath.Join(home, "Games") {
		t.Errorf("ExpandPath(~/Games) = %q", got)
	}
	if got := ExpandPath("  /x/y "); got != "/x/y" {
		t.Errorf("ExpandPath trims: %q", got)
	}
}

func TestManualGamesAreSavedAndRemoved(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()

	next, err := AddManualGame(dir, cfg, "", "/games/emulated")
	if err != nil {
		t.Fatal(err)
	}
	if len(next.ManualGames) != 1 || next.ManualGames[0].Name != "emulated" {
		t.Errorf("the name should default to the folder's: %+v", next.ManualGames)
	}
	if len(cfg.ManualGames) != 0 {
		t.Error("AddManualGame changed the config it was given")
	}
	loaded, err := config.Load(dir)
	if err != nil || len(loaded.ManualGames) != 1 {
		t.Fatalf("saved config = %+v, %v", loaded.ManualGames, err)
	}
	if _, err := AddManualGame(dir, next, "again", "/games/emulated"); err == nil {
		t.Error("the same folder was added twice")
	}

	gone, err := RemoveManualGame(dir, next, "/games/emulated")
	if err != nil || len(gone.ManualGames) != 0 {
		t.Errorf("RemoveManualGame = %+v, %v", gone.ManualGames, err)
	}
}

// Actions decide what an install, uninstall or adopt applies to.
func TestSplitTargetsAndReference(t *testing.T) {
	installed := state.Install{Exe: "a/g.exe", ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "normal", DLL: "dxgi.dll"}}
	e := GameEntry{
		Game: game.Game{ID: "steam:1", Name: "G", Root: "/nope"},
		Exes: []Executable{
			{Executable: game.Executable{Path: "b/g.exe", Arch: game.ArchX64, API: game.APID3D11}},
			{Executable: game.Executable{Path: "a/g.exe", Arch: game.ArchX64, API: game.APID3D11}, Installed: &installed},
		},
	}.WithGroups()
	e.Groups[0].Unmanaged = &install.AdoptCandidate{DLLName: "dxgi.dll"}

	inst, un := SplitTargets(e)
	if len(inst) != 1 || inst[0].Dir != "a" || len(un) != 1 || un[0].Dir != "b" {
		t.Fatalf("SplitTargets = %+v, %+v", inst, un)
	}
	// The reference folder is the one with the install, whatever the order.
	exe, ok := Reference(e.Groups)
	if !ok || exe.Path != "a/g.exe" {
		t.Errorf("Reference = %+v, %v", exe, ok)
	}
	if _, ok := Reference(nil); ok {
		t.Error("no folders, no reference")
	}

	ops := UninstallOps(e, e.Groups)
	if len(ops) != 1 || ops[0].Uninstall == nil || ops[0].Uninstall.Exe != "a/g.exe" || ops[0].Dir != "a" {
		t.Errorf("UninstallOps = %+v", ops)
	}
	if got := InstalledGroups(e); len(got) != 1 {
		t.Errorf("InstalledGroups = %d", len(got))
	}
}

func TestAdoptAll(t *testing.T) {
	e := GameEntry{
		Game: game.Game{ID: "steam:1", Name: "G", Root: "/nope"},
		Exes: []Executable{{Executable: game.Executable{Path: "g.exe", Arch: game.ArchX64}}},
	}.WithGroups()
	e.Groups[0].Unmanaged = &install.AdoptCandidate{DLLName: "dxgi.dll"}

	n, err := AdoptAll(fakeDeps(), e, e.Groups)
	if err != nil || n != 1 {
		t.Errorf("AdoptAll = %d, %v", n, err)
	}
	if _, err := AdoptAll(Deps{}, e, e.Groups); err == nil {
		t.Error("without an adopter there is nothing to adopt with")
	}
}

func TestUninstallDetailNamesTheRecordedFiles(t *testing.T) {
	const generic = "This removes only the files yarm created; anything you edited afterward is kept."
	if got := UninstallDetail("", "steam:1", nil); got != generic {
		t.Errorf("no state dir: %q", got)
	}
	if got := UninstallDetail(t.TempDir(), "steam:1", nil); got != generic {
		t.Errorf("no installs: %q", got)
	}
}
