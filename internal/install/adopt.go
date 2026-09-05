package install

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/state"
)

// knownDLLNames are the proxy DLL names ReShade's own installer uses
// (docs/plan/04-external-sources.md §4.6), in the order checked — a game
// folder installed by hand or another tool has exactly one of these next
// to the executable if it is running ReShade at all.
var knownDLLNames = []string{"dxgi.dll", "d3d11.dll", "d3d10.dll", "d3d12.dll", "d3d9.dll", "opengl32.dll"}

// AdoptCandidate is what a scan of an executable's directory found that
// looks like a ReShade install yarm did not create.
type AdoptCandidate struct {
	// DLLName is the proxy DLL found next to the executable.
	DLLName string
	// HasAddons is true when at least one *.addon32/*.addon64 file sits
	// beside the DLL — only the add-on build of ReShade loads them from
	// there, so their presence is what tells the two flavors apart after
	// the fact.
	HasAddons bool
	// ShaderFiles and TextureFiles are game-relative, slash-separated
	// paths under reshade-shaders/.
	ShaderFiles  []string
	TextureFiles []string
	// AddonFiles are game-relative paths of *.addon32/*.addon64 files.
	AddonFiles []string
	// HasD3DCompiler is true when d3dcompiler_47.dll sits beside the DLL
	// (the Linux/Proton case).
	HasD3DCompiler bool
}

// FileCount returns how many files Adopt would start tracking: the proxy
// DLL and ReShade.ini, plus everything Scan found.
func (c AdoptCandidate) FileCount() int {
	n := 2 // DLL + ini
	if c.HasD3DCompiler {
		n++
	}
	return n + len(c.ShaderFiles) + len(c.TextureFiles) + len(c.AddonFiles)
}

// ExeDir returns an executable's directory, game-relative and slash
// separated, or "" when it sits at the game root. ReShade intercepts by
// directory (whichever executable loads the proxy DLL sitting beside it),
// so this is also how callers group executables that share one ReShade
// install.
func ExeDir(exePath string) string {
	dir := path.Dir(filepath.ToSlash(exePath))
	if dir == "." {
		return ""
	}
	return dir
}

// ScanUnmanaged gathers everything Adopt needs to record. ok is false when
// root/exe.Path's directory does not actually look like a ReShade install:
// both a known proxy DLL and ReShade.ini are required, which is what tells
// a real (if unmanaged) install apart from a coincidentally named DLL with
// nothing to do with ReShade. The ReShade.ini check is a single stat call,
// so this is cheap to call even for a directory with nothing ReShade-shaped
// in it at all.
func ScanUnmanaged(root string, exe game.Executable) (candidate AdoptCandidate, ok bool) {
	exeDir := ExeDir(exe.Path)
	dir := filepath.Join(root, filepath.FromSlash(exeDir))

	if _, err := os.Stat(filepath.Join(dir, ININame)); err != nil {
		return AdoptCandidate{}, false
	}
	for _, name := range knownDLLNames {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			candidate.DLLName = name
			break
		}
	}
	if candidate.DLLName == "" {
		return AdoptCandidate{}, false
	}

	if _, err := os.Stat(filepath.Join(dir, artifacts.D3DCompiler)); err == nil {
		candidate.HasD3DCompiler = true
	}

	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			switch strings.ToLower(filepath.Ext(e.Name())) {
			case artifacts.ExtAddon32, artifacts.ExtAddon64:
				candidate.AddonFiles = append(candidate.AddonFiles, join(exeDir, e.Name()))
				candidate.HasAddons = true
			}
		}
	}

	// A missing reshade-shaders/Shaders or /Textures is normal (a bare
	// ReShade install with no effect packages yet) — walkFiles erroring
	// on either just means there is nothing to add here.
	_ = walkFiles(filepath.Join(dir, filepath.FromSlash(ShadersDir)), func(rel string, _ int64) error {
		candidate.ShaderFiles = append(candidate.ShaderFiles, join(exeDir, ShadersDir+"/"+rel))
		return nil
	})
	_ = walkFiles(filepath.Join(dir, filepath.FromSlash(TexturesDir)), func(rel string, _ int64) error {
		candidate.TextureFiles = append(candidate.TextureFiles, join(exeDir, TexturesDir+"/"+rel))
		return nil
	})

	return candidate, true
}

// Adopt records candidate as a yarm-managed install: it hashes every file
// candidate names, plus ReShade.ini and the proxy DLL itself, and writes
// an installs.json entry for g/exe. Nothing on disk changes — only the
// manifest gains an entry, so a later uninstall or upgrade through yarm
// works exactly as if yarm had installed it in the first place.
//
// The ReShade version cannot be recovered from the files alone, so it is
// recorded as "unknown"; that is display-only and does not affect
// uninstall, which works from the recorded file hashes, not the version.
func Adopt(stateDir string, g game.Game, exe game.Executable, candidate AdoptCandidate) (state.Install, error) {
	exeDir := ExeDir(exe.Path)

	var files []state.File
	add := func(rel string, origin state.Origin) error {
		abs, err := safeDest(g.Root, rel)
		if err != nil {
			return err
		}
		sum, err := hashFile(abs)
		if err != nil {
			return fmt.Errorf("hash %s: %w", rel, err)
		}
		size, err := fileSize(abs)
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}
		files = append(files, state.File{Path: rel, SHA256: sum, Size: size, Origin: origin})
		return nil
	}

	if err := add(join(exeDir, candidate.DLLName), state.OriginReShade); err != nil {
		return state.Install{}, err
	}
	if err := add(join(exeDir, ININame), state.OriginINI); err != nil {
		return state.Install{}, err
	}
	if candidate.HasD3DCompiler {
		if err := add(join(exeDir, artifacts.D3DCompiler), state.OriginD3DCompiler); err != nil {
			return state.Install{}, err
		}
	}
	for _, group := range [][]string{candidate.ShaderFiles, candidate.TextureFiles, candidate.AddonFiles} {
		for _, rel := range group {
			if err := add(rel, state.OriginAdopted); err != nil {
				return state.Install{}, err
			}
		}
	}

	flavor := FlavorNormal
	if candidate.HasAddons {
		flavor = FlavorAddon
	}

	in := state.Install{
		Exe:         filepath.ToSlash(exe.Path),
		InstalledAt: time.Now().UTC(),
		ReShade: state.ReShadeInfo{
			Version: "unknown (adopted)",
			Flavor:  string(flavor),
			Arch:    string(exe.Arch),
			API:     string(exe.API),
			DLL:     candidate.DLLName,
		},
		Files: files,
	}

	reg, err := state.Load(stateDir)
	if err != nil {
		return state.Install{}, err
	}
	reg.Record(g.ID, state.Game{Name: g.Name, Provider: g.Provider, Root: g.Root}, in)
	if err := state.Save(stateDir, reg); err != nil {
		return state.Install{}, fmt.Errorf("write manifest: %w", err)
	}

	return in, nil
}
