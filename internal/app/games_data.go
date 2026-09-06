package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

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
	// Runtime is what ReShade itself recorded about this folder — its own
	// version log, active preset, and effect files — gathered regardless
	// of whether yarm is tracking anything here yet.
	Runtime install.RuntimeInfo
}

// primaryExe is the executable an install should be tied to: the first
// one not flagged as an installer, crash handler or similar, falling back
// to the first executable at all if every one of them was. Used both for
// a freshly adopted install and to decide which executable a fresh
// install/update targets by default.
func (g FolderGroup) primaryExe() Executable {
	for _, e := range g.Exes {
		if !e.Skipped {
			return e
		}
	}
	return g.Exes[0]
}

// installedExe returns the executable Installed was actually recorded
// against, if it can still be found in this group — the one whose path
// yarm's install.Request named at install time, not necessarily the one
// currently highlighted in a table (ReShade applies to the whole folder,
// so any of its executables might be highlighted when the user asks to
// update or uninstall it).
// playableCount is how many of a folder's executables are actually
// offered — the skipped ones (uninstallers, redistributables) are not
// something ReShade would ever attach to.
func (g FolderGroup) playableCount() int {
	n := 0
	for _, e := range g.Exes {
		if !e.Skipped {
			n++
		}
	}
	return n
}

func (g FolderGroup) installedExe() (Executable, bool) {
	if g.Installed == nil {
		return Executable{}, false
	}
	for _, e := range g.Exes {
		if e.Path == g.Installed.Exe {
			return e, true
		}
	}
	return Executable{}, false
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
		exe := groups[i].primaryExe()
		if groups[i].Installed == nil {
			if candidate, ok := install.ScanUnmanaged(root, exe.Executable); ok {
				c := candidate
				groups[i].Unmanaged = &c
			}
		}
		groups[i].Runtime = install.InspectRuntime(root, exe.Path)
	}
	return groups
}

// anticheatWarning is shown wherever the add-on build is in play — it can
// load code beyond ReShade's own effects, which some anti-cheat systems
// treat as a cheat-tool signature; using it in an online or competitive
// game can get an account flagged or banned. Callers print it once, at
// the very bottom of a folder's whole block (after its executables, not
// sandwiched inside the ReShade status above them), so it reads as a
// standing notice rather than one more fact among several.
const anticheatWarning = "⚠ add-ons can trigger anti-cheat detection — avoid them in online or competitive games unless you know the game allows it"

// writeAnticheatWarning renders the warning as its own block: a blank line
// above and below, wrapped to width. Packed against the lines around it —
// a list of executables above, a row of key hints below — a red sentence
// reads as one more fact in the same block. The space is what makes it a
// notice.
func writeAnticheatWarning(b *strings.Builder, env Env, width int) {
	if width < 20 {
		width = 20
	}
	b.WriteString("\n")
	b.WriteString(env.Styles.Bad.Render(wrap(anticheatWarning, width)))
	b.WriteString("\n\n")
}

// groupIsAddon reports whether grp's install (or, if unmanaged, what was
// found) is the add-on build — the condition for showing anticheatWarning.
func groupIsAddon(grp FolderGroup) bool {
	switch {
	case grp.Installed != nil:
		return strings.EqualFold(grp.Installed.ReShade.Flavor, string(install.FlavorAddon))
	case grp.Unmanaged != nil:
		return grp.Unmanaged.HasAddons
	default:
		return false
	}
}

// reshadeStatusText returns what is installed (or found) in grp as plain,
// styled text, one fact per line, always starting with a "ReShade - "
// summary line so a caller can print it as a single self-contained block.
// hint is shown under an unmanaged finding, and differs by caller: the
// games list side panel says to open the detail screen first, the detail
// screen itself just says to press the key. Shared so the wording (and
// styling) of "what's installed" matches wherever it is shown.
func reshadeStatusText(grp FolderGroup, env Env, hint string, width int) string {
	// Every line is clipped before it is styled, because the block is
	// rendered inside a panel that wraps whatever does not fit — and a
	// wrapped line reads as a second, half-empty fact.
	line := func(style lipgloss.Style, text string) string {
		return style.Render(clipTail(text, width)) + "\n"
	}

	var b strings.Builder
	writeSectionHeader(&b, env, "ReShade", 0)

	switch {
	case grp.Installed != nil:
		in := grp.Installed
		b.WriteString(line(env.Styles.Good, fmt.Sprintf("  ✓ %s (%s)", in.ReShade.Version, in.ReShade.Flavor)))
		b.WriteString(line(env.Styles.Faint, "  "+dllWithCoverage(in.ReShade.DLL)))
		if in.ReShade.Version == install.AdoptedVersion && grp.Runtime.Version != "" {
			b.WriteString(line(env.Styles.Faint, "  last seen running: "+grp.Runtime.Version))
		}
	case grp.Unmanaged != nil:
		b.WriteString(line(env.Styles.Warn, "  ⚠ found, untracked"))
		b.WriteString(line(env.Styles.Faint, "  "+dllWithCoverage(grp.Unmanaged.DLLName)))
		if grp.Runtime.Version != "" {
			b.WriteString(line(env.Styles.Faint, "  last seen running: "+grp.Runtime.Version))
		}
		if hint != "" {
			b.WriteString(line(env.Styles.Accent, "  "+hint))
		}
	default:
		b.WriteString(line(env.Styles.Faint, "  not installed"))
	}

	if grp.Installed != nil {
		writeIndentedList(&b, env, "Shaders", grp.Installed.Packages, width)
		writeIndentedList(&b, env, "Add-ons", grp.Installed.Addons, width)
	}
	writeIndentedList(&b, env, "Enabled effects", grp.Runtime.ActiveTechniques, width)
	if n := len(grp.Runtime.AvailableEffects); n > 0 {
		b.WriteString(line(env.Styles.Faint, fmt.Sprintf("  %d effect file(s) available", n)))
	}

	// The first section starts the block, so it does not get the blank
	// line that separates one section from the last.
	return strings.TrimPrefix(b.String(), "\n")
}

// dllWithCoverage appends which graphics APIs a proxy DLL name covers
// ("dxgi.dll (D3D10 / D3D11 / D3D12)"), reusing the wizard's own dllOptions
// descriptions so the two never drift apart. Returns name unchanged when
// it is not one of ReShade's own proxy names.
func dllWithCoverage(name string) string {
	for _, o := range dllOptions {
		if o.Name == name {
			return name + " (" + strings.TrimSuffix(o.For, " (recommended)") + ")"
		}
	}
	return name
}

// writeIndentedList writes a labeled, indented list, one item per line,
// capped so a large preset or package selection cannot blow out the
// panel — the remainder is summarized as "+N more" instead of listed.
// Writes nothing when items is empty.
func writeIndentedList(b *strings.Builder, env Env, label string, items []string, width int) {
	if len(items) == 0 {
		return
	}
	const max = 8

	writeSectionHeader(b, env, label, len(items))
	shown := items
	if len(items) > max {
		shown = items[:max]
	}
	for _, it := range shown {
		b.WriteString(env.Styles.Faint.Render(clipTail("  "+it, width)))
		b.WriteString("\n")
	}
	if more := len(items) - len(shown); more > 0 {
		b.WriteString(env.Styles.Faint.Render(fmt.Sprintf("  +%d more", more)))
		b.WriteString("\n")
	}
}

// writeSectionHeader starts a section of the "what is in this folder"
// block: a blank line, then the title in the heading style with its count.
// Before this, every section title was the same faint gray as its own
// items and butted straight up against the previous section, so the block
// read as one long list of facts rather than as four groups of them.
func writeSectionHeader(b *strings.Builder, env Env, title string, count int) {
	b.WriteString("\n")
	if count > 0 {
		title = fmt.Sprintf("%s (%d)", title, count)
	}
	b.WriteString(env.Styles.Subtitle.Render(title))
	b.WriteString("\n")
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
