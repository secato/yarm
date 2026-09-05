package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/platform"
	"github.com/secato/yarm/internal/state"
)

// Executable pairs a discovered executable with its install status.
type Executable struct {
	game.Executable
	// Installed is the recorded install for this executable, if any.
	Installed *state.Install
}

// FolderGroup is every executable sharing one directory, plus the ReShade
// status that applies to all of them. ReShade intercepts by directory —
// the proxy DLL sits beside whichever executable loads it — so a folder
// with three executables has exactly one ReShade status, not three.
type FolderGroup struct {
	// Dir is the shared directory, game-relative and slash-separated (""
	// for the game root itself).
	Dir  string
	Exes []Executable
	// Installed is the recorded install applying to this folder, taken
	// from whichever of its executables yarm installed for.
	Installed *state.Install
	// Unmanaged holds what was found when this folder has a ReShade
	// install on disk that yarm is not tracking yet (a manual install, or
	// one made by another tool). nil when there is nothing to adopt —
	// either something is already tracked, or there is nothing
	// ReShade-shaped here at all.
	Unmanaged *install.AdoptCandidate
}

// primaryExe is the executable an adopted (or newly recorded) install
// should be tied to: the first one not flagged as an installer, crash
// handler or similar, falling back to the first executable at all if
// every one of them was.
func (g FolderGroup) primaryExe() Executable {
	for _, e := range g.Exes {
		if !e.Skipped {
			return e
		}
	}
	return g.Exes[0]
}

// groupByFolder groups exes sharing a directory, in order of first
// appearance, and works out each group's ReShade status: the recorded
// install if any of its executables has one, otherwise a cheap probe for
// an unmanaged install already on disk. Safe to call with a root that does
// not exist (as fixture-built GameEntry values in tests do): the probe's
// stat calls simply fail and the group is left with nothing to adopt.
func groupByFolder(root string, exes []Executable) []FolderGroup {
	var groups []FolderGroup
	index := map[string]int{}
	for _, ex := range exes {
		dir := install.ExeDir(ex.Path)
		i, ok := index[dir]
		if !ok {
			i = len(groups)
			index[dir] = i
			groups = append(groups, FolderGroup{Dir: dir})
		}
		groups[i].Exes = append(groups[i].Exes, ex)
		if ex.Installed != nil && groups[i].Installed == nil {
			installed := *ex.Installed
			groups[i].Installed = &installed
		}
	}

	for i := range groups {
		if groups[i].Installed != nil {
			continue
		}
		if candidate, ok := install.ScanUnmanaged(root, groups[i].primaryExe().Executable); ok {
			c := candidate
			groups[i].Unmanaged = &c
		}
	}
	return groups
}

// writeReShadeStatus writes what is installed (or found) in grp as plain
// text lines, styled and ready to append to a strings.Builder. hint is
// shown under an unmanaged finding, and differs by caller: the games list
// side panel says to open the detail screen first, the detail screen
// itself just says to press the key. Shared so the wording (and styling)
// of "what's installed" matches wherever it is shown.
func writeReShadeStatus(b *strings.Builder, grp FolderGroup, env Env, hint string) {
	switch {
	case grp.Installed != nil:
		in := grp.Installed
		b.WriteString(env.Styles.Good.Render(
			fmt.Sprintf("✓ %s (%s) — %s", in.ReShade.Version, in.ReShade.Flavor, in.ReShade.DLL)))
		b.WriteString("\n")
		if len(in.Packages) > 0 {
			b.WriteString(env.Styles.Faint.Render("packages: " + strings.Join(in.Packages, ", ")))
			b.WriteString("\n")
		}
		if len(in.Addons) > 0 {
			b.WriteString(env.Styles.Faint.Render("add-ons: " + strings.Join(in.Addons, ", ")))
			b.WriteString("\n")
		}
	case grp.Unmanaged != nil:
		b.WriteString(env.Styles.Warn.Render("⚠ found, untracked (" + grp.Unmanaged.DLLName + ")"))
		b.WriteString("\n")
		if hint != "" {
			b.WriteString(env.Styles.Faint.Render(hint))
			b.WriteString("\n")
		}
	default:
		b.WriteString(env.Styles.Faint.Render("not installed"))
		b.WriteString("\n")
	}
}

// apiLabel renders a guessed graphics API as the friendlier label shown in
// the UI ("DirectX 12" rather than the raw "d3d12" tag game.Inspect uses
// internally).
func apiLabel(api game.API) string {
	switch api {
	case game.APID3D8:
		return "DirectX 8"
	case game.APID3D9:
		return "DirectX 9"
	case game.APID3D10:
		return "DirectX 10"
	case game.APID3D11:
		return "DirectX 11"
	case game.APID3D12:
		return "DirectX 12"
	case game.APIDXGI:
		return "DirectX (DXGI)"
	case game.APIOpenGL:
		return "OpenGL"
	case game.APIVulkan:
		return "Vulkan"
	default:
		return "unknown"
	}
}

// GameEntry is a discovered game as the UI shows it.
type GameEntry struct {
	game.Game
	Exes   []Executable
	Groups []FolderGroup
	// NativeBuild marks a game that ships no Windows executable, so
	// ReShade cannot apply to it at all.
	NativeBuild bool
	// ScanErr is set when the game's folder could not be scanned at all
	// (most commonly a permissions problem). Kept distinct from
	// NativeBuild/an empty Exes so the UI can say what actually happened
	// instead of the folder just looking empty.
	ScanErr error
}

// InstalledSummary describes the first install found for this game, for
// the list's status column. Returns "" when nothing is installed.
func (g GameEntry) InstalledSummary() string {
	for _, grp := range g.Groups {
		if grp.Installed != nil {
			return grp.Installed.ReShade.Version + " " + grp.Installed.ReShade.Flavor
		}
	}
	return ""
}

// HasUnmanaged reports whether any folder in this game looks like it
// already has an unmanaged ReShade install.
func (g GameEntry) HasUnmanaged() bool {
	for _, grp := range g.Groups {
		if grp.Unmanaged != nil {
			return true
		}
	}
	return false
}

// GroupFor returns the folder group containing exePath, if any.
func (g GameEntry) GroupFor(exePath string) (FolderGroup, bool) {
	for _, grp := range g.Groups {
		for _, e := range grp.Exes {
			if e.Path == exePath {
				return grp, true
			}
		}
	}
	return FolderGroup{}, false
}

// PlayableExes returns the executables worth offering, hiding the ones the
// scanner flagged as installers or crash handlers.
func (g GameEntry) PlayableExes() []Executable {
	out := make([]Executable, 0, len(g.Exes))
	for _, e := range g.Exes {
		if !e.Skipped {
			out = append(out, e)
		}
	}
	return out
}

// GamesLoader discovers games for the UI. It is an interface so tests can
// supply fixtures instead of touching a real Steam library.
type GamesLoader interface {
	LoadGames(ctx context.Context) ([]GameEntry, error)
}

// ProviderLoader builds GameEntries from platform providers, scanning each
// game for executables and matching them against recorded installs.
type ProviderLoader struct {
	Providers []platform.Provider
	// StateDir holds installs.json.
	StateDir string
}

// LoadGames implements GamesLoader.
func (l ProviderLoader) LoadGames(ctx context.Context) ([]GameEntry, error) {
	found, err := platform.DiscoverAll(ctx, l.Providers)
	if err != nil {
		// Discovery errors are partial by design: one provider failing
		// should not hide the games another found, so they are surfaced
		// only when nothing at all turned up.
		if len(found) == 0 {
			return nil, err
		}
	}

	reg, err := state.Load(l.StateDir)
	if err != nil {
		return nil, err
	}

	entries := make([]GameEntry, 0, len(found))
	for _, g := range found {
		entry := GameEntry{Game: g}

		exes, err := game.Scan(g.Root)
		if err != nil {
			// A game folder we cannot read is still worth listing — the
			// detail view says why, rather than it just looking empty
			// (the plan's "friendly errors: permission denied on game
			// dir" case).
			exes = nil
			entry.ScanErr = err
		}

		for _, e := range exes {
			arch, api := game.Inspect(filepath.Join(g.Root, e.Path))
			e.Arch, e.API = arch, api

			ex := Executable{Executable: e}
			if in, ok := reg.FindInstall(g.ID, filepath.ToSlash(e.Path)); ok {
				installed := in
				ex.Installed = &installed
			}
			entry.Exes = append(entry.Exes, ex)
		}
		entry.Groups = groupByFolder(g.Root, entry.Exes)

		// A folder we could not even scan is not worth also probing for a
		// native build — that call would likely fail the same way, and
		// the scan error is the more useful thing to show anyway.
		if len(entry.Exes) == 0 && entry.ScanErr == nil {
			entry.NativeBuild = game.HasNativeBuild(g.Root)
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}
