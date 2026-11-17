package core

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// onDisk is a game whose folder really exists, for what looks at files.
func onDisk(t *testing.T, files map[string]string, installed *state.Install) (GameEntry, Executable) {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	exe := Executable{
		Executable: game.Executable{Path: "Game/g.exe", Arch: game.ArchX64, API: game.APIDXGI},
		Installed:  installed,
	}
	e := GameEntry{
		Game: game.Game{ID: "steam:9", Name: "Disk Game", Provider: "steam", Root: root},
		Exes: []Executable{exe},
	}.WithGroups()
	return e, e.Exes[0]
}

// A file that is not ours where the DLL goes is a conflict; once recorded
// as ours and unchanged it is not worth mentioning.
func TestSessionConflictsSeparateForeignFromOurs(t *testing.T) {
	e, exe := onDisk(t, map[string]string{"Game/dxgi.dll": "someone else's injector"}, nil)
	s := NewSession(e, exe, e.Groups, fakeDeps(), sampleWizardData())

	cs := s.Conflicts()
	if len(cs) != 1 || cs[0].Kind != install.ConflictForeign || cs[0].Path != "Game/dxgi.dll" {
		t.Fatalf("Conflicts() = %+v", cs)
	}
	if !s.HasForeign() {
		t.Error("HasForeign() = false")
	}
	if got := DescribeConflict(cs[0], s.Overwrite, s.Backup); got == "" {
		t.Error("a conflict should be describable")
	}

	// Ops turns Overwrite off where nothing foreign was seen, so the default
	// of "replace" never reaches a file the user was never asked about.
	e2, exe2 := onDisk(t, map[string]string{"Game/other.txt": "x"}, nil)
	s2 := NewSession(e2, exe2, e2.Groups, fakeDeps(), sampleWizardData())
	ops, _ := s2.Ops()
	if ops[0].Install.Overwrite {
		t.Error("Overwrite stayed on with nothing in the way")
	}
	ops, _ = s.Ops()
	if !ops[0].Install.Overwrite {
		t.Error("Overwrite was dropped with a foreign file in the way")
	}

	s.SetOverwrite(false)
	s.SetBackup(false)
	if req, _ := s.Request(exe); req.Overwrite || !req.NoBackup {
		t.Errorf("request after the switches = %+v", req)
	}
}

func TestSessionManagedFilesThatWillBeKeptAreNotConflicts(t *testing.T) {
	body := "reshade dll bytes"
	installed := &state.Install{
		Exe:     "Game/g.exe",
		ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "addon", Arch: "x64", API: "dxgi", DLL: "dxgi.dll"},
		Files:   []state.File{{Path: "Game/dxgi.dll", SHA256: sum([]byte(body))}},
	}
	e, exe := onDisk(t, map[string]string{"Game/dxgi.dll": body}, installed)
	s := NewSession(e, exe, e.Groups, fakeDeps(), sampleWizardData())

	if cs := s.Conflicts(); len(cs) != 0 {
		t.Fatalf("an intact managed file is a conflict: %+v", cs)
	}

	// Edited since: the plan will replace it, which is worth saying.
	if err := os.WriteFile(filepath.Join(e.Root, "Game", "dxgi.dll"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	if cs := s.Conflicts(); len(cs) != 1 || cs[0].Kind != install.ConflictManaged {
		t.Errorf("after an edit: %+v", cs)
	}

	// Changing the version means it will be replaced regardless of its hash.
	if err := os.WriteFile(filepath.Join(e.Root, "Game", "dxgi.dll"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	s.SetVersion("6.7.3")
	if cs := s.Conflicts(); len(cs) != 1 {
		t.Errorf("a version change should replace the DLL: %+v", cs)
	}
}

func TestSessionAddonRowsAndFolderControls(t *testing.T) {
	e, exe := freshEntry()
	s := newTestSession(t, e, exe, fakeDeps())
	if s.AddonTotal() != 3 { // swap chain override, renodx entry, plus the RenoDX utility list is empty
		t.Logf("addon total = %d", s.AddonTotal())
	}
	if len(s.AddonRows(true)) == 0 {
		t.Error("no add-on rows")
	}
	if !s.HasRenoDX() {
		t.Error("the fixture has RenoDX mods")
	}
	if RenoDXMinReShade() == "" {
		t.Error("RenoDXMinReShade is empty")
	}
	if got := s.Arch(); got != game.ArchX64 {
		t.Errorf("Arch() = %q", got)
	}
	if len(s.Warnings()) != len(s.Unmet()) {
		t.Error("Warnings is Unmet")
	}
	if s.ExeFor(e.Groups[0]).Path != exe.Path {
		t.Error("ExeFor")
	}
	// CycleExe is a no-op for a folder with nothing to cycle to.
	s.CycleExe(e.Groups[0].Dir)
	if s.Folders.ExeFor(e.Groups[0]).Path != exe.Path {
		t.Error("CycleExe moved off the only executable")
	}
}

func TestGameEntryHelpers(t *testing.T) {
	installed := state.Install{Exe: "a/g.exe", ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "addon", DLL: "dxgi.dll"}}
	e := GameEntry{
		Game: game.Game{ID: "x", Name: "X", Root: "/nope/Game X"},
		Exes: []Executable{
			{Executable: game.Executable{Path: "a/g.exe", Arch: game.ArchX64, API: game.APID3D12}, Installed: &installed},
			{Executable: game.Executable{Path: "b/s.exe", Arch: game.ArchX86, API: game.APIUnknown, Skipped: true}},
			{Executable: game.Executable{Path: "c/h.exe", Arch: game.ArchX86, API: game.APIOpenGL}},
		},
	}.WithGroups()

	if got := e.InstalledSummary(); got != "6.8.0 addon" {
		t.Errorf("InstalledSummary() = %q", got)
	}
	if e.HasUnmanaged() {
		t.Error("HasUnmanaged() with nothing on disk")
	}
	if g, ok := e.GroupFor("c/h.exe"); !ok || g.Dir != "c" {
		t.Errorf("GroupFor = %+v, %v", g, ok)
	}
	if _, ok := e.GroupFor("zzz.exe"); ok {
		t.Error("GroupFor found a missing exe")
	}
	if len(e.PlayableExes()) != 2 {
		t.Errorf("PlayableExes() = %d, want the skipped one left out", len(e.PlayableExes()))
	}
	if got := SortedDetailGroups(e.Groups); got[0].Dir != "a" {
		t.Errorf("the installed folder should lead, got %q", got[0].Dir)
	}
	if got := DetailFolderLabel(e, "a"); got != "Game X/a/" {
		t.Errorf("DetailFolderLabel = %q", got)
	}
	if got := FolderLabel(""); got != "(game root)/" {
		t.Errorf("FolderLabel = %q", got)
	}
	if got := FolderSummary(e.Groups[0]); got != "ReShade 6.8.0 (addon)" {
		t.Errorf("FolderSummary = %q", got)
	}
	if got := FolderSummary(e.Groups[2]); got != "no ReShade" {
		t.Errorf("FolderSummary = %q", got)
	}
	lines, hidden := FolderExes(e.Groups[0], "a/g.exe")
	if len(lines) != 1 || !lines[0].Chosen || hidden != 0 || lines[0].Name != "g.exe" {
		t.Errorf("FolderExes = %+v, %d", lines, hidden)
	}
	for api, want := range map[game.API]string{
		game.APID3D9: "DirectX 9", game.APID3D11: "DirectX 11", game.APID3D12: "DirectX 12", game.APIDXGI: "DirectX (DXGI)",
		game.APIOpenGL: "OpenGL", game.APIVulkan: "Vulkan", game.APIUnknown: "unknown", game.APID3D8: "DirectX 8", game.APID3D10: "DirectX 10",
	} {
		if got := APILabel(api); got != want {
			t.Errorf("APILabel(%q) = %q, want %q", api, got, want)
		}
	}
	if got := DLLWithCoverage("dxgi.dll"); got == "dxgi.dll" {
		t.Errorf("DLLWithCoverage should name what it covers: %q", got)
	}
	if got := DLLWithCoverage("custom.dll"); got != "custom.dll" {
		t.Errorf("DLLWithCoverage of a non-proxy = %q", got)
	}
}
