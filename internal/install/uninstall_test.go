package install

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/state"
)

// snapshot records every file under root with its hash, so two states of
// a directory tree can be compared exactly.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return out
}

// dirsUnder lists every directory under root, relative and sorted.
func dirsUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(out)
	return out
}

// This is step 4's "done when": installing into a game directory and then
// uninstalling must leave it byte-identical to how it started.
func TestInstallUninstallRoundTripIsByteIdentical(t *testing.T) {
	for _, target := range []artifacts.TargetOS{artifacts.OSWindows, artifacts.OSLinux} {
		t.Run(string(target), func(t *testing.T) {
			f := newFixture(t).WithReShade().WithD3DCompiler().
				WithPackage("standard-effects",
					artifacts.PackageMeta{Name: "Standard", EffectFiles: []string{"Deband.fx"}},
					map[string]string{
						"Shaders/Deband.fx":   "// deband",
						"Shaders/ReShade.fxh": "// header",
						"Textures/noise.png":  "png bytes",
					}).
				WithAddon("swapchain", map[string]string{"swapchain.addon64": "addon bin"}).
				WithCustom("mine", map[string]string{"Shaders/Mine.fx": "// mine"})

			// The game as shipped, including files YARM must not touch.
			f.GameFile("Game/emberhollow.exe", "the game binary")
			f.GameFile("Game/data/assets.pak", "game assets")
			f.GameFile("readme.txt", "game readme")

			before := snapshot(t, f.GameDir)
			dirsBefore := dirsUnder(t, f.GameDir)

			req := f.Request()
			req.TargetOS = target
			req.Packages = []string{"standard-effects"}
			req.Addons = []string{"swapchain"}
			req.Custom = []string{"mine"}

			res, err := planAndRun(t, f, req, state.Registry{})
			if err != nil {
				t.Fatalf("install: %v", err)
			}
			if len(res.Written) == 0 {
				t.Fatal("nothing was installed")
			}
			// Sanity: the install really did change the directory.
			if cmp.Diff(before, snapshot(t, f.GameDir)) == "" {
				t.Fatal("install made no changes to the game directory")
			}

			out, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
				GameID: req.Game.ID,
				Exe:    "Game/emberhollow.exe",
			})
			if err != nil {
				t.Fatalf("uninstall: %v", err)
			}
			if len(out.Kept) != 0 {
				t.Errorf("Kept = %v, want nothing kept when no file was edited", out.Kept)
			}

			if diff := cmp.Diff(before, snapshot(t, f.GameDir)); diff != "" {
				t.Errorf("game directory is not byte-identical after uninstall (-before +after):\n%s", diff)
			}
			if diff := cmp.Diff(dirsBefore, dirsUnder(t, f.GameDir)); diff != "" {
				t.Errorf("directories left behind after uninstall (-before +after):\n%s", diff)
			}

			// The registry must no longer claim the install.
			reg, err := state.Load(f.StateDir)
			if err != nil {
				t.Fatalf("Load(): %v", err)
			}
			if _, ok := reg.FindInstall(req.Game.ID, "Game/emberhollow.exe"); ok {
				t.Error("the install is still recorded after uninstall")
			}
		})
	}
}

// A file the user edited after installation is theirs now.
func TestUninstallKeepsModifiedFiles(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("pkg", artifacts.PackageMeta{Name: "P"},
			map[string]string{"Shaders/A.fx": "// original"})

	req := f.Request()
	req.Packages = []string{"pkg"}
	if _, err := planAndRun(t, f, req, state.Registry{}); err != nil {
		t.Fatalf("install: %v", err)
	}

	const edited = "// the user tuned this shader"
	f.GameFile("Game/reshade-shaders/Shaders/A.fx", edited)

	out, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: req.Game.ID, Exe: "Game/emberhollow.exe",
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	if !slices.Contains(out.Kept, "Game/reshade-shaders/Shaders/A.fx") {
		t.Errorf("Kept = %v, want the edited shader", out.Kept)
	}
	if got := f.read("Game/reshade-shaders/Shaders/A.fx"); got != edited {
		t.Errorf("the user's edit was destroyed: %q", got)
	}
	if f.exists("Game/dxgi.dll") {
		t.Error("an unmodified file should still have been removed")
	}
}

// Presets and logs are the user's own work and survive by default.
func TestUninstallPreservesUserData(t *testing.T) {
	f := newFixture(t).WithReShade()
	req := f.Request()
	if _, err := planAndRun(t, f, req, state.Registry{}); err != nil {
		t.Fatalf("install: %v", err)
	}

	const preset = "[Deband.fx]\nStrength=1.5\n"
	f.GameFile("Game/"+PresetName, preset)
	f.GameFile("Game/"+LogName, "log output")

	out, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: req.Game.ID, Exe: "Game/emberhollow.exe",
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	_ = out

	if !f.exists("Game/" + PresetName) {
		t.Error("ReShadePreset.ini was deleted; it is the user's work")
	}
	if got := f.read("Game/" + PresetName); got != preset {
		t.Errorf("preset content changed: %q", got)
	}
	if !f.exists("Game/" + LogName) {
		t.Error("ReShade.log was deleted without being asked for")
	}
}

// ...unless the user explicitly asks for them to go.
func TestUninstallRemovesUserDataOnRequest(t *testing.T) {
	f := newFixture(t).WithReShade()
	req := f.Request()
	if _, err := planAndRun(t, f, req, state.Registry{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	f.GameFile("Game/"+PresetName, "preset")
	f.GameFile("Game/"+LogName, "log")

	if _, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: req.Game.ID, Exe: "Game/emberhollow.exe", RemoveUserData: true,
	}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	if f.exists("Game/" + PresetName) {
		t.Error("ReShadePreset.ini survived an explicit removal request")
	}
	if f.exists("Game/" + LogName) {
		t.Error("ReShade.log survived an explicit removal request")
	}
}

// A displaced file comes back where it was.
func TestUninstallRestoresBackup(t *testing.T) {
	f := newFixture(t).WithReShade()
	const original = "the original dxgi.dll"
	f.GameFile("Game/dxgi.dll", original)

	before := snapshot(t, f.GameDir)

	req := f.Request()
	req.Overwrite = true
	if _, err := planAndRun(t, f, req, state.Registry{}); err != nil {
		t.Fatalf("install: %v", err)
	}

	out, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: req.Game.ID, Exe: "Game/emberhollow.exe",
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !slices.Contains(out.Restored, "Game/dxgi.dll") {
		t.Errorf("Restored = %v, want the displaced file", out.Restored)
	}
	if got := f.read("Game/dxgi.dll"); got != original {
		t.Errorf("dxgi.dll = %q, want the original back", got)
	}
	if f.exists("Game/dxgi.dll" + BackupSuffix) {
		t.Error("the backup file was left behind")
	}
	if diff := cmp.Diff(before, snapshot(t, f.GameDir)); diff != "" {
		t.Errorf("directory not restored (-before +after):\n%s", diff)
	}
}

// A file already gone is reported, not an error.
func TestUninstallToleratesMissingFiles(t *testing.T) {
	f := newFixture(t).WithReShade()
	req := f.Request()
	if _, err := planAndRun(t, f, req, state.Registry{}); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := os.Remove(filepath.Join(f.GameDir, "Game", "dxgi.dll")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	out, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: req.Game.ID, Exe: "Game/emberhollow.exe",
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !slices.Contains(out.Missing, "Game/dxgi.dll") {
		t.Errorf("Missing = %v, want the deleted file reported", out.Missing)
	}
}

func TestUninstallUnknownInstall(t *testing.T) {
	f := newFixture(t)
	u := NewUninstaller(f.StateDir)

	if _, err := u.Run(UninstallRequest{GameID: "steam:999", Exe: "x.exe"}); err == nil {
		t.Error("want an error for an unknown game, got nil")
	}

	var reg state.Registry
	reg.Record("steam:1", state.Game{Root: f.GameDir}, state.Install{Exe: "a.exe"})
	if err := state.Save(f.StateDir, reg); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	if _, err := u.Run(UninstallRequest{GameID: "steam:1", Exe: "b.exe"}); err == nil {
		t.Error("want an error for an unknown exe, got nil")
	}
}

// Uninstalling one executable must not disturb another install in the
// same game.
func TestUninstallLeavesSiblingInstall(t *testing.T) {
	f := newFixture(t).WithReShade()

	first := f.Request()
	if _, err := planAndRun(t, f, first, state.Registry{}); err != nil {
		t.Fatalf("install 1: %v", err)
	}

	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	second := f.Request()
	second.Exe.Path = filepath.FromSlash("Other/game2.exe")
	if _, err := planAndRun(t, f, second, reg); err != nil {
		t.Fatalf("install 2: %v", err)
	}

	if _, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: first.Game.ID, Exe: "Game/emberhollow.exe",
	}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	if f.exists("Game/dxgi.dll") {
		t.Error("the uninstalled executable's DLL survived")
	}
	if !f.exists("Other/dxgi.dll") {
		t.Error("uninstalling one executable removed another install's files")
	}

	reg, err = state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if _, ok := reg.FindInstall(first.Game.ID, "Other/game2.exe"); !ok {
		t.Error("the sibling install was dropped from the registry")
	}
}

// An upgrade must not disown the ReShade.ini an earlier install created.
// Before this was fixed, the second install dropped the ini from the
// manifest, so uninstall left it behind permanently.
func TestUpgradeKeepsOwnershipOfINI(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("pkg", artifacts.PackageMeta{Name: "P"},
			map[string]string{"Shaders/A.fx": "a"})

	before := snapshot(t, f.GameDir)
	dirsBefore := dirsUnder(t, f.GameDir)

	req := f.Request()
	req.Packages = []string{"pkg"}
	if _, err := planAndRun(t, f, req, state.Registry{}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if !f.exists("Game/" + ININame) {
		t.Fatal("the first install did not create ReShade.ini")
	}

	// Second install over the top: the ini already exists.
	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	res, err := planAndRun(t, f, req, reg)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	var hasINI bool
	for _, fl := range res.Install.Files {
		if fl.Origin == state.OriginINI {
			hasINI = true
		}
	}
	if !hasINI {
		t.Error("the upgrade's manifest no longer lists ReShade.ini")
	}

	if _, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: req.Game.ID, Exe: "Game/emberhollow.exe",
	}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	if f.exists("Game/" + ININame) {
		t.Error("ReShade.ini survived uninstall after an upgrade")
	}
	if diff := cmp.Diff(before, snapshot(t, f.GameDir)); diff != "" {
		t.Errorf("not byte-identical after install/upgrade/uninstall (-before +after):\n%s", diff)
	}
	if diff := cmp.Diff(dirsBefore, dirsUnder(t, f.GameDir)); diff != "" {
		t.Errorf("directories left behind (-before +after):\n%s", diff)
	}
}

// An ini the user edited after we created it is theirs, and survives.
func TestUninstallKeepsEditedINI(t *testing.T) {
	f := newFixture(t).WithReShade()
	req := f.Request()
	if _, err := planAndRun(t, f, req, state.Registry{}); err != nil {
		t.Fatalf("install: %v", err)
	}

	const tuned = "[GENERAL]\nEffectSearchPaths=.\\my-own-path\n"
	f.GameFile("Game/"+ININame, tuned)

	// Upgrade over the top, then uninstall.
	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if _, err := planAndRun(t, f, req, reg); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	out, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: req.Game.ID, Exe: "Game/emberhollow.exe",
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	if !slices.Contains(out.Kept, "Game/"+ININame) {
		t.Errorf("Kept = %v, want the edited ini", out.Kept)
	}
	if got := f.read("Game/" + ININame); got != tuned {
		t.Errorf("the user's ini was destroyed: %q", got)
	}
}

// An ini that existed before yarm ever ran is never adopted.
func TestUninstallLeavesPreExistingINI(t *testing.T) {
	f := newFixture(t).WithReShade()
	const preExisting = "[GENERAL]\nSomeoneElsesConfig=1\n"
	f.GameFile("Game/"+ININame, preExisting)

	req := f.Request()
	if _, err := planAndRun(t, f, req, state.Registry{}); err != nil {
		t.Fatalf("install: %v", err)
	}

	res, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	in, _ := res.FindInstall(req.Game.ID, "Game/emberhollow.exe")
	for _, fl := range in.Files {
		if fl.Origin == state.OriginINI {
			t.Error("yarm claimed ownership of an ini it did not create")
		}
	}

	if _, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: req.Game.ID, Exe: "Game/emberhollow.exe",
	}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if got := f.read("Game/" + ININame); got != preExisting {
		t.Errorf("a pre-existing ini was modified or removed: %q", got)
	}
}

// The whole point of recording a RenoDX mod like any other file: it comes
// back out again. Nothing reads the origin to do it — the manifest's path
// and hash are enough — which is exactly why a new origin was safe to add.
func TestUninstallRemovesARenoDXMod(t *testing.T) {
	f := newFixture(t).WithReShade().WithRenoDX("cp2077", "renodx-cp2077.addon64")

	req := f.Request()
	req.RenoDX = "cp2077"
	if _, err := planAndRun(t, f, req, state.Registry{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !f.exists("Game/renodx-cp2077.addon64") {
		t.Fatal("setup: the mod was never installed")
	}

	// It is recorded against the install, so the wizard can preselect it
	// when the install is edited later.
	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("state.Load(): %v", err)
	}
	in, ok := reg.FindInstall(req.Game.ID, "Game/emberhollow.exe")
	if !ok {
		t.Fatal("no install recorded")
	}
	if in.RenoDX != "cp2077" {
		t.Errorf("recorded RenoDX = %q, want cp2077", in.RenoDX)
	}

	out, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: req.Game.ID, Exe: "Game/emberhollow.exe",
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !slices.Contains(out.Removed, "Game/renodx-cp2077.addon64") {
		t.Errorf("Removed = %v, want the RenoDX mod", out.Removed)
	}
	if f.exists("Game/renodx-cp2077.addon64") {
		t.Error("the mod is still in the game folder after uninstall")
	}
}

// forgedRegistry records one file with a caller-chosen origin, path and
// content, all shaped to pass Load validation — the shape the uninstall
// guards themselves must then catch.
func forgedRegistry(t *testing.T, f *fixture, root string, origin state.Origin, rel, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(filepath.FromSlash(rel))), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	reg := state.Registry{}
	reg.Record("steam:700110", state.Game{Name: "Ember Hollow", Provider: "steam", Root: root}, state.Install{
		Exe:     "Game/emberhollow.exe",
		ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "normal", DLL: "dxgi.dll"},
		Files: []state.File{{
			Path: rel, SHA256: sha256Of(content), Size: int64(len(content)), Origin: origin,
		}},
	})
	if err := state.Save(f.StateDir, reg); err != nil {
		t.Fatalf("Save(): %v", err)
	}
}

// A manifest entry shaped like nothing yarm writes is skipped and
// reported, even when its hash matches: the hash gate alone cannot catch
// an entry whose content the forger supplied too.
func TestUninstallSkipsMisshapenEntries(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game binary")
	forgedRegistry(t, f, f.GameDir, state.PackageOrigin("x"), "Game/savestate.dat", "precious save")

	out, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: "steam:700110", Exe: "Game/emberhollow.exe",
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !slices.Contains(out.Kept, "Game/savestate.dat") {
		t.Errorf("Kept = %v, want the misshapen entry reported", out.Kept)
	}
	if !f.exists("Game/savestate.dat") {
		t.Error("a misshapen manifest entry must never be deleted")
	}
}

// A manifest entry pointing at a symlink is kept: yarm only ever writes
// regular files.
func TestUninstallSkipsSymlinks(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game binary")
	target := f.GameFile("Game/real.dll", "dll bytes")
	link := filepath.Join(f.GameDir, "Game", "dxgi.dll")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	forgedRegistry(t, f, f.GameDir, state.OriginReShade, "Game/dxgi.dll", "dll bytes")

	out, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: "steam:700110", Exe: "Game/emberhollow.exe",
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !slices.Contains(out.Kept, "Game/dxgi.dll") {
		t.Errorf("Kept = %v, want the symlink reported", out.Kept)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("the link itself should be untouched: %v", err)
	}
}

// A registry whose root no longer holds the recorded install refuses to
// run, instead of deleting another folder's files.
func TestUninstallRefusesAlienRoot(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game binary")
	forgedRegistry(t, f, f.GameDir, state.OriginReShade, "Game/dxgi.dll", "dll bytes")

	other := t.TempDir()
	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	in, ok := reg.FindInstall("steam:700110", "Game/emberhollow.exe")
	if !ok {
		t.Fatal("no install recorded")
	}
	reg.Remove("steam:700110", "Game/emberhollow.exe")
	reg.Record("steam:700110", state.Game{Name: "Ember Hollow", Provider: "steam", Root: other}, in)
	if err := state.Save(f.StateDir, reg); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	if _, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: "steam:700110", Exe: "Game/emberhollow.exe",
	}); err == nil {
		t.Error("want an error for a root without the recorded install, got nil")
	}
	if !f.exists("Game/dxgi.dll") {
		t.Error("nothing may be deleted when the root check fails")
	}
}

// A game folder deleted wholesale leaves nothing to remove: every entry
// reports missing and the record is dropped, rather than erroring.
func TestUninstallMissingRootDropsRecord(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game binary")
	forgedRegistry(t, f, f.GameDir, state.OriginReShade, "Game/dxgi.dll", "dll bytes")

	if err := os.RemoveAll(f.GameDir); err != nil {
		t.Fatalf("remove game dir: %v", err)
	}
	out, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: "steam:700110", Exe: "Game/emberhollow.exe",
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !slices.Contains(out.Missing, "Game/dxgi.dll") {
		t.Errorf("Missing = %v, want the entry reported", out.Missing)
	}
	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if _, ok := reg.FindInstall("steam:700110", "Game/emberhollow.exe"); ok {
		t.Error("the record should be dropped once there is nothing left to remove")
	}
}

// A backup swapped for a symlink after the install is never restored
// over the game folder.
func TestUninstallSkipsSymlinkedBackup(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game binary")
	forgedRegistry(t, f, f.GameDir, state.OriginReShade, "Game/dxgi.dll", "dll bytes")

	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	in, ok := reg.FindInstall("steam:700110", "Game/emberhollow.exe")
	if !ok {
		t.Fatal("no install recorded")
	}
	// A displaced original, then swapped for a link before uninstall.
	realBak := f.GameFile("Game/dxgi.dll.yarm-bak", "the original")
	linkBak := filepath.Join(f.GameDir, "Game", "dxgi.dll.yarm-bak")
	if err := os.Remove(linkBak); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.Symlink(realBak, linkBak); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	in.Backups = []state.Backup{{Path: "Game/dxgi.dll", Backup: "Game/dxgi.dll.yarm-bak"}}
	gm := reg.Games["steam:700110"]
	reg.Remove("steam:700110", "Game/emberhollow.exe")
	reg.Record("steam:700110", gm, in)
	if err := state.Save(f.StateDir, reg); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	// Remove the installed DLL so the restore path is reached (restore
	// only happens over an absent file).
	if err := os.Remove(filepath.Join(f.GameDir, "Game", "dxgi.dll")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	out, err := NewUninstaller(f.StateDir).Run(UninstallRequest{
		GameID: "steam:700110", Exe: "Game/emberhollow.exe",
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(out.Restored) != 0 {
		t.Errorf("Restored = %v, want nothing restored from a linked backup", out.Restored)
	}
	if f.exists("Game/dxgi.dll") {
		t.Error("a linked backup must never be renamed over the game folder")
	}
}
