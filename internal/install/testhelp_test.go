package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/game"
)

// fixture builds a realistic cache and game directory pair for a test.
type fixture struct {
	t        *testing.T
	CacheDir string
	GameDir  string
	StateDir string
	Art      Artifacts
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{
		t:        t,
		CacheDir: filepath.Join(base, "cache"),
		GameDir:  filepath.Join(base, "game"),
		StateDir: filepath.Join(base, "state"),
	}
	for _, d := range []string{f.CacheDir, f.GameDir, f.StateDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	f.Art = Artifacts{
		Packages: map[string]string{},
		Addons:   map[string]string{},
		Custom:   map[string]string{},
	}
	return f
}

// write creates a file with content, relative to dir.
func (f *fixture) write(dir, rel, content string) string {
	f.t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// WithReShade puts ReShade32/64.dll in the cache.
func (f *fixture) WithReShade() *fixture {
	f.t.Helper()
	dir := filepath.Join(f.CacheDir, "reshade", "6.8.0", "addon")
	f.write(dir, artifacts.ReShade32, "reshade 32-bit body")
	f.write(dir, artifacts.ReShade64, "reshade 64-bit body")
	f.Art.ReShadeDir = dir
	return f
}

// WithD3DCompiler puts a stand-in d3dcompiler_47.dll in the cache.
func (f *fixture) WithD3DCompiler() *fixture {
	f.t.Helper()
	dir := filepath.Join(f.CacheDir, "d3dcompiler", "x64")
	f.Art.D3DCompiler = f.write(dir, artifacts.D3DCompiler, "d3dcompiler body")
	return f
}

// WithPackage adds a normalized package cache entry. files are paths
// relative to the entry, e.g. "Shaders/A.fx".
func (f *fixture) WithPackage(id string, meta artifacts.PackageMeta, files map[string]string) *fixture {
	f.t.Helper()
	dir := filepath.Join(f.CacheDir, "packages", id, "20260905-abcdefg")
	for rel, content := range files {
		f.write(dir, rel, content)
	}
	meta.ID = id
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		f.t.Fatalf("marshal meta: %v", err)
	}
	f.write(dir, artifacts.MetaFile, string(raw))
	f.Art.Packages[id] = dir
	return f
}

// WithAddon adds a cached add-on directory.
func (f *fixture) WithAddon(id string, files map[string]string) *fixture {
	f.t.Helper()
	dir := filepath.Join(f.CacheDir, "addons", id, "20260905-x64")
	for rel, content := range files {
		f.write(dir, rel, content)
	}
	f.Art.Addons[id] = dir
	return f
}

// WithCustom adds a user-managed content folder.
func (f *fixture) WithCustom(id string, files map[string]string) *fixture {
	f.t.Helper()
	dir := filepath.Join(f.CacheDir, "custom", "shaders", id)
	for rel, content := range files {
		f.write(dir, rel, content)
	}
	f.Art.Custom[id] = dir
	return f
}

// GameFile creates a pre-existing file inside the game directory.
func (f *fixture) GameFile(rel, content string) string {
	f.t.Helper()
	return f.write(f.GameDir, rel, content)
}

// Request returns a baseline install request against this fixture.
func (f *fixture) Request() Request {
	return Request{
		Game: game.Game{
			ID: "steam:700110", Name: "Ember Hollow", Provider: "steam", Root: f.GameDir,
		},
		Exe:      game.Executable{Path: filepath.FromSlash("Game/emberhollow.exe"), Arch: game.ArchX64, API: game.APID3D12},
		Version:  "6.8.0",
		Flavor:   FlavorAddon,
		DLLName:  "dxgi.dll",
		TargetOS: artifacts.OSWindows,
	}
}

// dests returns the plan's destinations, for order-independent assertions.
func dests(p Plan) []string {
	out := make([]string, 0, len(p.Files))
	for _, f := range p.Files {
		out = append(out, f.Dest)
	}
	return out
}

// actionFor returns the planned action for a destination.
func actionFor(t *testing.T, p Plan, dest string) Action {
	t.Helper()
	for _, f := range p.Files {
		if f.Dest == dest {
			return f.Action
		}
	}
	t.Fatalf("no planned file for %q; have %v", dest, dests(p))
	return ""
}

// exists reports whether a game-relative path is present.
func (f *fixture) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(f.GameDir, filepath.FromSlash(rel)))
	return err == nil
}

// read returns a game-relative file's contents.
func (f *fixture) read(rel string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.GameDir, filepath.FromSlash(rel)))
	if err != nil {
		f.t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}
