package install

import (
	"os"
	"path/filepath"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/state"
)

// The planner already decides what happens to a file that is in the way
// (§5.2: keep it, or back it up and replace it) — but it decides that
// while the install is running, after everything has been downloaded, and
// reports it in the result. By then the user has already answered the only
// question that mattered: whether to overwrite.
//
// Preflight answers it beforehand. It covers only the files whose names
// are known before anything is downloaded — the proxy DLL, the D3D
// compiler, ReShade's own ini — which is also where conflicts actually
// happen: a shader landing in reshade-shaders/Shaders collides with
// nothing, while dxgi.dll is exactly the name every other injector wants
// too.

// ConflictKind is what yarm found sitting where it wants to write.
type ConflictKind string

// ConflictKind values.
const (
	// ConflictManaged is a file a recorded install of ours owns: it is
	// simply replaced, and needs no permission.
	ConflictManaged ConflictKind = "managed"
	// ConflictForeign is a file yarm did not write. It is kept (and the
	// install left half-useless) unless Overwrite is set, in which case it
	// is renamed aside and restored on uninstall.
	ConflictForeign ConflictKind = "foreign"
	// ConflictConfig is ReShade.ini, which is the user's configuration
	// once it exists and is never overwritten either way.
	ConflictConfig ConflictKind = "config"
)

// Conflict is one existing file in the way of an install.
type Conflict struct {
	// Path is game-relative and slash-separated, matching state.File.
	Path string
	Kind ConflictKind
	// Size is the existing file's size, which is often what identifies it
	// — a 25 MB dxgi.dll is not a stale ReShade.
	Size int64
	// ReShade marks a foreign file that does look like a ReShade install
	// (an unmanaged one, next to a ReShade.ini), as opposed to some other
	// program that wanted the same DLL name.
	ReShade bool
}

// Blocking reports whether this conflict stops the install from working as
// asked — a foreign file that will be kept rather than replaced.
func (c Conflict) Blocking(overwrite bool) bool {
	return c.Kind == ConflictForeign && !overwrite
}

// Preflight lists what already exists where req would write, without
// downloading anything: one stat per known name. prev is the install
// already recorded for this folder, if any, so files yarm itself put there
// are not reported as somebody else's.
func Preflight(req Request, prev *state.Install) []Conflict {
	dir := filepath.Join(req.Game.Root, filepath.FromSlash(req.ExeDir()))
	// A ReShade.ini beside the DLL is what tells an unmanaged ReShade
	// install apart from an unrelated program using the same DLL name —
	// the same test ScanUnmanaged uses to decide what can be adopted.
	_, iniErr := os.Stat(filepath.Join(dir, ININame))
	looksLikeReShade := iniErr == nil

	names := []string{req.DLLName}
	if artifacts.NeedsD3DCompiler(req.TargetOS) {
		names = append(names, artifacts.D3DCompiler)
	}
	names = append(names, ININame)

	var out []Conflict
	for _, name := range names {
		rel := join(req.ExeDir(), name)
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() {
			continue
		}

		c := Conflict{Path: rel, Size: info.Size()}
		switch {
		case name == ININame:
			c.Kind = ConflictConfig
		case owns(prev, rel):
			c.Kind = ConflictManaged
		default:
			c.Kind, c.ReShade = ConflictForeign, looksLikeReShade
		}
		out = append(out, c)
	}
	return out
}

// owns reports whether a recorded install claims rel.
func owns(prev *state.Install, rel string) bool {
	if prev == nil {
		return false
	}
	_, ok := prev.OwnedFile(rel)
	return ok
}
