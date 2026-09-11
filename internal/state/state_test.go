package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func sampleInstall(exe string) Install {
	return Install{
		Exe:         exe,
		InstalledAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		ReShade: ReShadeInfo{
			Version: "6.8.0", Flavor: "addon", Arch: "x64", API: "d3d12", DLL: "dxgi.dll",
		},
		Packages: []string{"standard-effects"},
		Files: []File{
			{Path: "Game/dxgi.dll", SHA256: strings.Repeat("ab", 32), Size: 100, Origin: OriginReShade},
		},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	reg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() on a fresh dir: %v", err)
	}
	if len(reg.Games) != 0 {
		t.Errorf("fresh registry has %d games, want 0", len(reg.Games))
	}

	reg.Record("steam:700110", Game{
		Name: "Ember Hollow", Provider: "steam", Root: "/games/EH",
	}, sampleInstall("Game/emberhollow.exe"))

	if err := Save(dir, reg); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if diff := cmp.Diff(reg, got); diff != "" {
		t.Errorf("round trip mismatch (-want +got):\n%s", diff)
	}
	if got.Schema != SchemaVersion {
		t.Errorf("schema = %d, want %d", got.Schema, SchemaVersion)
	}
}

func TestRecordReplacesSameExe(t *testing.T) {
	var reg Registry
	g := Game{Name: "G", Provider: "steam", Root: "/games/g"}

	reg.Record("steam:1", g, sampleInstall("a.exe"))
	reg.Record("steam:1", g, sampleInstall("b.exe"))

	updated := sampleInstall("a.exe")
	updated.ReShade.Version = "6.9.0"
	reg.Record("steam:1", g, updated)

	installs := reg.Games["steam:1"].Installs
	if len(installs) != 2 {
		t.Fatalf("got %d installs, want 2 (same exe should replace, not append)", len(installs))
	}
	if installs[0].Exe != "a.exe" || installs[1].Exe != "b.exe" {
		t.Errorf("installs not sorted by exe: %q, %q", installs[0].Exe, installs[1].Exe)
	}
	if installs[0].ReShade.Version != "6.9.0" {
		t.Errorf("version = %q, want the updated 6.9.0", installs[0].ReShade.Version)
	}
}

// A game can move between Steam libraries; the registry must follow it.
func TestRecordRefreshesGameIdentity(t *testing.T) {
	var reg Registry
	reg.Record("steam:1", Game{Name: "G", Provider: "steam", Root: "/old"}, sampleInstall("a.exe"))
	reg.Record("steam:1", Game{Name: "G Renamed", Provider: "steam", Root: "/new"}, sampleInstall("b.exe"))

	g := reg.Games["steam:1"]
	if g.Root != "/new" {
		t.Errorf("Root = %q, want /new", g.Root)
	}
	if g.Name != "G Renamed" {
		t.Errorf("Name = %q", g.Name)
	}
	if len(g.Installs) != 2 {
		t.Errorf("got %d installs, want both retained", len(g.Installs))
	}
}

func TestRemove(t *testing.T) {
	var reg Registry
	g := Game{Name: "G", Provider: "steam", Root: "/games/g"}
	reg.Record("steam:1", g, sampleInstall("a.exe"))
	reg.Record("steam:1", g, sampleInstall("b.exe"))

	if !reg.Remove("steam:1", "a.exe") {
		t.Error("Remove() = false, want true")
	}
	if n := len(reg.Games["steam:1"].Installs); n != 1 {
		t.Errorf("got %d installs, want 1", n)
	}

	// Removing the last install drops the game entirely.
	if !reg.Remove("steam:1", "b.exe") {
		t.Error("Remove() = false for the last install")
	}
	if _, ok := reg.Games["steam:1"]; ok {
		t.Error("game should be dropped once it has no installs")
	}

	if reg.Remove("steam:1", "a.exe") {
		t.Error("Remove() on a missing game = true, want false")
	}
}

func TestFindInstallAndOwnedFile(t *testing.T) {
	var reg Registry
	reg.Record("steam:1", Game{Root: "/g"}, sampleInstall("a.exe"))

	in, ok := reg.FindInstall("steam:1", "a.exe")
	if !ok {
		t.Fatal("FindInstall() = false")
	}
	f, ok := in.OwnedFile("Game/dxgi.dll")
	if !ok {
		t.Fatal("OwnedFile() = false for a recorded file")
	}
	if f.SHA256 != strings.Repeat("ab", 32) {
		t.Errorf("SHA256 = %q", f.SHA256)
	}
	if _, ok := in.OwnedFile("Game/other.dll"); ok {
		t.Error("OwnedFile() = true for a file we never wrote")
	}
	if _, ok := reg.FindInstall("steam:1", "missing.exe"); ok {
		t.Error("FindInstall() = true for an unknown exe")
	}
}

// The registry is the only record of what YARM put in the user's game
// directories, so a corrupt one must be reported rather than silently
// replaced with an empty one that would strand those files.
func TestLoadRejectsCorruptRegistry(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("want an error for a corrupt registry, got nil")
	}
}

func TestLoadRejectsNewerSchema(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(`{"schema":99,"games":{}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("want an error for a newer schema, got nil")
	}
}

// validRegistry is a loadable registry: one game, one install, one file,
// all values shaped the way yarm writes them.
func validRegistry() Registry {
	reg := Registry{Schema: SchemaVersion, Games: map[string]Game{}}
	reg.Record("steam:700110", Game{
		Name: "Ember Hollow", Provider: "steam", Root: "/games/EH",
	}, sampleInstall("Game/emberhollow.exe"))
	return reg
}

// withFiles returns reg with the install's file list replaced.
func withFiles(reg Registry, files []File) Registry {
	g := reg.Games["steam:700110"]
	in := g.Installs[0]
	in.Files = files
	g.Installs = []Install{in}
	reg.Games["steam:700110"] = g
	return reg
}

// writeUnsigned stores reg without a signature, so tests exercise
// validation rather than verification.
func writeUnsigned(t *testing.T, dir string, reg Registry) {
	t.Helper()
	raw, err := json.Marshal(reg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(Path(dir), raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// A saved registry is signed: the file carries an HMAC, and flipping a
// single byte breaks verification on load.
func TestSaveSignsLoadVerifies(t *testing.T) {
	dir := t.TempDir()

	if err := Save(dir, validRegistry()); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	raw, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), `"hmac":`) {
		t.Errorf("saved registry should carry a signature:\n%s", raw)
	}
	if _, err := Load(dir); err != nil {
		t.Errorf("Load() of a signed registry: %v", err)
	}

	// Tamper with one byte of a path: verification must fail closed.
	tampered := strings.Replace(string(raw), "dxgi.dll", "dxgi.dlX", 1)
	if tampered == string(raw) {
		t.Fatal("test setup failed to alter the file")
	}
	if err := os.WriteFile(Path(dir), []byte(tampered), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(dir); !errors.Is(err, ErrIntegrity) {
		t.Errorf("Load() of a tampered registry = %v, want %v", err, ErrIntegrity)
	}
}

// Files from before signing carry no HMAC: they load trust-on-first-use
// and pick up a signature on the next save.
func TestLegacyUnsignedRegistryLoads(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"schema":1,"games":{"steam:700110":{"name":"Ember Hollow","provider":"steam","root":"/games/EH","installs":[]}}}`
	if err := os.WriteFile(Path(dir), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	reg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() of an unsigned registry: %v", err)
	}
	if len(reg.Games) != 1 {
		t.Errorf("got %d games, want 1", len(reg.Games))
	}
	if err := Save(dir, reg); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	if _, err := Load(dir); err != nil {
		t.Errorf("Load() after re-signing: %v", err)
	}
	raw, _ := os.ReadFile(Path(dir))
	if !strings.Contains(string(raw), `"hmac":`) {
		t.Error("re-saved registry should be signed")
	}
}

// A signed registry without its key is unverifiable: fail closed, with
// recovery guidance, rather than acting on it.
func TestSignedRegistryWithoutKeyFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, validRegistry()); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	if err := os.Remove(filepath.Join(dir, keyFileName)); err != nil {
		t.Fatalf("remove key: %v", err)
	}
	if _, err := Load(dir); !errors.Is(err, ErrIntegrity) {
		t.Errorf("Load() without the key = %v, want %v", err, ErrIntegrity)
	}
}

// The key file is owner-only: it is the one secret in the data dir.
func TestKeyFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("owner-only bits do not apply on Windows")
	}
	dir := t.TempDir()
	if err := Save(dir, Registry{}); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, keyFileName))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file perms = %o, want 600", perm)
	}
}

// Validation fails registries yarm could not have written: absolute and
// escaping paths, non-hash digests, negative sizes, relative roots.
func TestLoadRejectsMalformedRegistry(t *testing.T) {
	file := File{Path: "Game/dxgi.dll", SHA256: strings.Repeat("ab", 32), Size: 100, Origin: OriginReShade}

	withRoot := func(reg Registry, root string) Registry {
		g := reg.Games["steam:700110"]
		g.Root = root
		reg.Games["steam:700110"] = g
		return reg
	}

	cases := map[string]Registry{
		"absolute path": withFiles(validRegistry(), []File{{
			Path: "/etc/dxgi.dll", SHA256: file.SHA256, Size: 100, Origin: OriginReShade,
		}}),
		"escaping path": withFiles(validRegistry(), []File{{
			Path: "Game/../../evil.dll", SHA256: file.SHA256, Size: 100, Origin: OriginReShade,
		}}),
		"backslash path": withFiles(validRegistry(), []File{{
			Path: `Game\evil.dll`, SHA256: file.SHA256, Size: 100, Origin: OriginReShade,
		}}),
		"non-hash digest": withFiles(validRegistry(), []File{{
			Path: file.Path, SHA256: "notahash", Size: 100, Origin: OriginReShade,
		}}),
		"negative size": withFiles(validRegistry(), []File{{
			Path: file.Path, SHA256: file.SHA256, Size: -1, Origin: OriginReShade,
		}}),
		"relative root": withRoot(validRegistry(), "games/EH"),
	}

	for name, reg := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeUnsigned(t, dir, reg)
			if _, err := Load(dir); err == nil {
				t.Errorf("want a validation error for %s, got nil", name)
			}
		})
	}
}

func TestInstallsSorted(t *testing.T) {
	var reg Registry
	reg.Record("steam:2", Game{Root: "/b"}, sampleInstall("z.exe"))
	reg.Record("steam:1", Game{Root: "/a"}, sampleInstall("b.exe"))
	reg.Record("steam:1", Game{Root: "/a"}, sampleInstall("a.exe"))

	got := reg.Installs()
	want := []struct{ id, exe string }{
		{"steam:1", "a.exe"}, {"steam:1", "b.exe"}, {"steam:2", "z.exe"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d installs, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].GameID != w.id || got[i].Install.Exe != w.exe {
			t.Errorf("[%d] = %s/%s, want %s/%s", i, got[i].GameID, got[i].Install.Exe, w.id, w.exe)
		}
	}
}

func TestOriginHelpers(t *testing.T) {
	if got := PackageOrigin("sweetfx"); got != Origin("package:sweetfx") {
		t.Errorf("PackageOrigin() = %q", got)
	}
	if got := AddonOrigin("rest"); got != Origin("addon:rest") {
		t.Errorf("AddonOrigin() = %q", got)
	}
	if got := CustomOrigin("mine"); got != Origin("custom:mine") {
		t.Errorf("CustomOrigin() = %q", got)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	var reg Registry
	reg.Record("steam:1", Game{Root: "/g"}, sampleInstall("a.exe"))

	if err := Save(dir, reg); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		// installs.key is the registry's signature key, written
		// alongside it on first save.
		if filepath.Ext(e.Name()) == ".tmp" || (e.Name() != FileName && e.Name() != keyFileName) {
			t.Errorf("unexpected leftover file %q", e.Name())
		}
	}
}
