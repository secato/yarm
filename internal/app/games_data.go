package app

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/platform"
	"github.com/secato/yarm/internal/state"
)

// Executable pairs a discovered executable with its install status.
type Executable struct {
	game.Executable
	// Installed is the recorded install for this executable, if any.
	Installed *state.Install
}

// GameEntry is a discovered game as the UI shows it.
type GameEntry struct {
	game.Game
	Exes []Executable
	// NativeBuild marks a game that ships no Windows executable, so
	// ReShade cannot apply to it at all.
	NativeBuild bool
}

// InstalledSummary describes the first install found for this game, for
// the list's status column. Returns "" when nothing is installed.
func (g GameEntry) InstalledSummary() string {
	for _, e := range g.Exes {
		if e.Installed != nil {
			return e.Installed.ReShade.Version + " " + e.Installed.ReShade.Flavor
		}
	}
	return ""
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
			// A game folder we cannot read is still worth listing; the
			// detail view will show it has no executables.
			exes = nil
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

		if len(entry.Exes) == 0 {
			entry.NativeBuild = game.HasNativeBuild(g.Root)
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}
